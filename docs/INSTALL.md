# Installing Scouter on a phone

The short version is the [Quick start](../README.md#quick-start). This page
has every step, the one-time adb setup and the signing details.

You need an Android 9+ phone with USB debugging on, `adb` on your computer
(`platform-tools` from the Android SDK, or your distribution's package), and
a running [aggregator](../README.md#aggregator) reachable over HTTPS.

## 1. Get the APK

**Download** `scouter-app_<version>_debug-signed.apk` from the
[latest release](https://github.com/drilonrecica/scouter/releases/latest).
It is a release build (minified) signed with the CI runner's debug key, see
[Signing](#signing) for what that means.

**Or build it** (needs a JDK 17+ with `javac` and the Android SDK, found
through `ANDROID_HOME` or `sdk.dir` in `android/local.properties`):

```sh
cd android && ./gradlew :app:assembleRelease
# -> app/build/outputs/apk/release/app-release.apk
```

## 2. Install

```sh
adb install -r scouter-app_<version>_debug-signed.apk
```

`adb install -r` keeps the app's data when you update. Android installs an
update only over the same signing key: see [Signing](#signing) if it
answers `INSTALL_FAILED_UPDATE_INCOMPATIBLE` (uninstall first, then install).

## 3. One-time setup (adb)

```sh
adb shell appops set dev.recica.scouter SYSTEM_ALERT_WINDOW allow      # may bring itself forward for alerts
adb shell dpm set-active-admin dev.recica.scouter/.AdminReceiver       # may switch the screen off
adb shell settings put global stay_on_while_plugged_in 0            # the app keeps the screen on itself
adb shell locksettings set-disabled true                            # no lock screen in front of alerts
adb shell dumpsys deviceidle whitelist +dev.recica.scouter          # Doze would stall the schedule overnight
```

Each line is optional in the sense that the app runs without it; together
they make it a dashboard that stays up on its own. `set-active-admin` is
needed for the screen schedule, `deviceidle whitelist` for it to stay on
time overnight (the settings screen shows whether it is still missing).

## 4. Point it at your aggregator

```sh
# quote the schedule: it contains a space
adb shell "am broadcast -n dev.recica.scouter/.ConfigReceiver --es url https://scouter.example.com --es token SECRET --es schedule '1-5 09:00-19:00'"
adb shell am start -n dev.recica.scouter/.DashboardActivity
```

`token` is the aggregator's `SCOUTER_TOKEN`. `schedule` is
`<first>-<last> <on>-<off>`: a range of days, `1`=Monday to `7`=Sunday, and
the screen's on and off times (`HH:MM`). The on time must be before the off
time: overnight schedules are not supported. Default `1-5 09:00-19:00`.

Configuration is accepted only from adb: the receiver requires
`android.permission.DUMP`, which other apps cannot hold, so no app on the
phone can redirect it to a server of its own and collect the token. A new
`url` is only accepted together with a `token`. Plain HTTP is allowed only
for `127.0.0.1`/`localhost`; everything else must be HTTPS.

The phone should show the Focus screen within a few seconds. If it says
`NO SIGNAL`, check the URL from the phone's browser
(`https://scouter.example.com/healthz` should answer `ok <commit>`), and
the token.

## Developing against a local aggregator

```sh
cd aggregator && SCOUTER_TOKEN=dev SCOUTER_GITHUB_TOKEN=$(gh auth token) SCOUTER_ADDR=:8787 make run
adb reverse tcp:8787 tcp:8787
adb shell "am broadcast -n dev.recica.scouter/.ConfigReceiver --es url http://127.0.0.1:8787 --es token dev"
```

## Signing

The APK on the Releases page is **debug-signed**: the release workflow has
no key of its own, so Gradle signs the build with the runner's freshly
generated debug keystore. That is fine for sideloading, with two
consequences:

- Every release is signed with a *different* throwaway key, so updating from
  one downloaded release to the next needs `adb uninstall dev.recica.scouter`
  first (release device ownership before that: settings screen → *Release
  device owner*). Over-the-air updates between downloaded releases do not
  work for the same reason.
- An APK you build yourself is signed with *your* machine's
  `~/.android/debug.keystore`, which stays the same across builds, so your
  own builds update each other in place and over the air. Back that file up:
  if it is lost, the app has to be reinstalled over adb.

**A release-signed APK.** Gradle signs with a keystore of your choice when
these variables are set; otherwise it uses the debug key:

```sh
keytool -genkeypair -v -keystore scouter.keystore -alias scouter -keyalg RSA -keysize 4096 -validity 10000
export SCOUTER_KEYSTORE_FILE=$PWD/scouter.keystore SCOUTER_KEYSTORE_PASSWORD=... SCOUTER_KEY_ALIAS=scouter SCOUTER_KEY_PASSWORD=...
cd android && ./gradlew :app:assembleRelease
```

The release workflow does the same when the repository has the secrets
`SCOUTER_KEYSTORE_BASE64` (`base64 -w0 scouter.keystore`),
`SCOUTER_KEYSTORE_PASSWORD`, `SCOUTER_KEY_ALIAS` and `SCOUTER_KEY_PASSWORD`;
the asset is then named `scouter-app_<version>.apk` without the
`_debug-signed` suffix, and successive releases update each other in place.
`~/.android/debug.keystore` works as that keystore too (password `android`,
alias `androiddebugkey`), which keeps CI releases compatible with a phone
that already runs your local builds.
