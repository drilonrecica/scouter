#!/usr/bin/env bash
# Release the phone app over the air:
#   bump versionCode (+1) and the patch version, build the release APK,
#   publish it to the aggregator, commit the bump and tag it v<version>,
#   and wait until the phone reports it runs it.
#
#   tools/release.sh            # 0.3.3 -> 0.3.4
#   tools/release.sh 0.4.0      # explicit version name
#
# Needs ~/.config/scouter/url and ~/.config/scouter/release-token (0600), and
# a build signed with the same key as the installed app (this machine's).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GRADLE_FILE="$ROOT/android/app/build.gradle.kts"
CONF="$HOME/.config/scouter"
URL=$(cat "$CONF/url")
[ -r "$CONF/release-token" ] || { echo "missing $CONF/release-token" >&2; exit 1; }

# The tag must name exactly what was built: no uncommitted app changes.
if ! git -C "$ROOT" diff --quiet HEAD -- android/ || [ -n "$(git -C "$ROOT" ls-files --others --exclude-standard -- android/)" ]; then
  echo "android/ has uncommitted changes; commit them first so the release tag matches the APK" >&2
  exit 1
fi

code=$(grep -oP 'versionCode = \K[0-9]+' "$GRADLE_FILE")
name=$(grep -oP 'versionName = "\K[^"]+' "$GRADLE_FILE")
new_code=$((code + 1))
new_name=${1:-$(echo "$name" | awk -F. '{print $1"."$2"."$3+1}')}
sed -i "s/versionCode = $code/versionCode = $new_code/; s/versionName = \"$name\"/versionName = \"$new_name\"/" "$GRADLE_FILE"
echo "version $name ($code) -> $new_name ($new_code)"
# Until it is published and committed, a failure undoes the bump.
trap 'git -C "$ROOT" checkout -q -- android/app/build.gradle.kts; echo "release failed; version bump undone" >&2' EXIT

# Any JDK with javac; Android Studio's bundled one if JAVA_HOME has none.
if [ ! -x "${JAVA_HOME:-/nonexistent}/bin/javac" ]; then
  for j in "$HOME/.local/share/JetBrains/Toolbox/apps/android-studio/jbr" /opt/android-studio/jbr; do
    [ -x "$j/bin/javac" ] && export JAVA_HOME="$j" && break
  done
fi
(cd "$ROOT/android" && ./gradlew -q :app:testDebugUnitTest :app:assembleRelease)
APK="$ROOT/android/app/build/outputs/apk/release/app-release.apk"
echo "built $(du -h "$APK" | cut -f1) APK"

# The token goes in through a pipe, never on a command line (ps would show it).
auth() { printf 'Authorization: Bearer %s' "$(tr -d '\n' < "$CONF/release-token")"; }
curl -fsS -X POST -H @<(auth) -H "Content-Type: application/vnd.android.package-archive" \
  --data-binary @"$APK" "$URL/v1/release" >/dev/null
git -C "$ROOT" commit -q -m "Release $new_name" -- android/app/build.gradle.kts
trap - EXIT
git -C "$ROOT" tag -a "v$new_name" -m "Phone app $new_name (versionCode $new_code)"
echo "published; committed and tagged v$new_name (push with: git push --follow-tags)"
echo "waiting for the phone (it checks with every state update and heartbeat)..."

for _ in $(seq 1 36); do
  running=$(curl -fsS -H @<(auth) "$URL/v1/release" | python3 -c 'import sys, json
p = json.load(sys.stdin).get("phone") or {}
print(p.get("app_version", "?"), "|", p.get("last_ota", ""))')
  if [ "${running%% |*}" = "$new_name" ]; then
    echo "phone runs $new_name ($running)"
    exit 0
  fi
  sleep 5
done
echo "phone still reports: $running (it may be offline; it updates once it reconnects)" >&2
exit 2
