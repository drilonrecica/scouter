# Scouter

> *"It's over 9000!"* (failed builds, hopefully not)

Reads your CI's power level. A retired Samsung Galaxy S3 Neo sits on the desk
and shows the CI status of your most active GitHub repos on its AMOLED
screen, with the RGB notification LED as a build light.

```
GitHub API ──► aggregator (Go, on Coolify) ──SSE──► phone app (Kotlin)
                                                    ├─ Focus / Grid screens
                                                    └─ RGB LED
```

- **`aggregator/`** polls GitHub (with ETags, so quiet repos cost nothing),
  combines each commit's workflows into one status, and streams a small JSON
  document to the phone. Standard library only.
- **`android/`** renders that document as a Scouter HUD. No dependencies, 116 KB APK, about 26 MB RAM.

## What the phone shows

| | |
|---|---|
| **Focus** | One project large: CI status of the default branch, workflow, commit, duration, newer runs on other branches, open PRs (bots excluded), power level. Follows your latest push (`TRACKING`); long-press to pin (`LOCKED`). |
| **Grid** | The 9 most active projects, failing ones first, with power levels and the average. Tap a tile to focus it. |
| **Alert** | A *new* failure on a default branch (less than 12 h old) cracks the lens: `POWER LEVEL DROPPING`, `PWR 9000 → 6750`. Takes over the screen for a minute, then stays as a red strip until tapped. Wakes the screen when it is off. |
| **Power level** | Build health, 0–9000: the share of the last 20 finished default-branch commits that passed (a commit passes when all its workflows do). `----` until there is a record. Every Grid project at 9000 earns an `IT'S OVER 9000!`. |
| **LED** (screen off) | red = undismissed alert · amber = a build is running · green = all quiet · purple = no connection |

Swipe left/right to change screens. The screen is on during the schedule
(default Mon–Fri 09:00–19:00) and off otherwise, for burn-in and power.

Long-standing red builds are deliberately **not** alerts: they show red in
Grid, but only fresh breakage lights the LED. Hide repos you don't care about
with `SCOUTER_IGNORE_REPOS`.

## Design

The phone is a Scouter: a HUD on black. The rules that keep it cheap on an
AMOLED panel that is on all day:

- **Black stays black.** Chrome (lens frame, reticle brackets, labels) is
  thin teal lines, deliberately bluer than the green that means "passing".
- **Status is never decoration.** Status words stay large and in their
  colours; the HUD only frames them.
- **Animation only on change.** A new state sweeps a scan line over the lens
  and counts changed power levels for under a second; otherwise nothing moves
  except a ±4 px burn-in shift once a minute.

Inspired by the Scouter from Dragon Ball; not affiliated with or endorsed by
Toei Animation or Shueisha. No official artwork is used: everything is drawn
in code. Font: [Share Tech Mono](https://fonts.google.com/specimen/Share+Tech+Mono)
by Carrois Type Design, SIL Open Font License 1.1 (`android/licenses/`).

## Aggregator

| Env var | |
|---|---|
| `SCOUTER_TOKEN` | required: bearer token the phone sends |
| `SCOUTER_GITHUB_TOKEN` | required: fine-grained PAT, read-only *Actions*, *Contents*, *Pull requests*, *Metadata* on all your repos |
| `SCOUTER_IGNORE_REPOS` | comma-separated `owner/repo` list to hide |
| `SCOUTER_ADDR` | default `:8080` |
| `SCOUTER_DATA_DIR` | default `/data`; holds `state.json` so restarts don't start empty |

Endpoints: `GET /v1/stream` (SSE, full state on every change, ping every 25 s),
`GET /v1/state` (with ETag), `GET /healthz`.

**Coolify:** new resource → this repo, build pack *Dockerfile*, base directory
`/aggregator`, port 8080, a persistent volume on `/data`, the env vars above as
secrets. Leave Coolify's health check **off**: the image is distroless (no
`curl`/`wget` inside), and Coolify runs its checks inside the container, so
an enabled check fails every deploy. Check `/healthz` from outside instead.

Local run: `cd aggregator && SCOUTER_TOKEN=dev SCOUTER_GITHUB_TOKEN=$(gh auth token) make run`

## Phone

Built against the S3 Neo running LineageOS 19.1 (userdebug, so `adb root` works).

```sh
cd android && ./gradlew :app:assembleRelease      # needs a JDK with javac (17+)
adb install -r app/build/outputs/apk/release/app-release.apk

# one-time setup
adb shell appops set dev.recica.scouter SYSTEM_ALERT_WINDOW allow      # may bring itself forward for alerts
adb shell dpm set-active-admin dev.recica.scouter/.AdminReceiver       # may switch the screen off
adb shell settings put global stay_on_while_plugged_in 0            # the app keeps the screen on itself
adb shell locksettings set-disabled true                            # no lock screen in front of alerts

# configure (quote the schedule: it contains a space)
adb shell "am broadcast -n dev.recica.scouter/.ConfigReceiver --es url https://scouter.example.com --es token SECRET --es schedule '1-5 09:00-19:00'"
adb shell am start -n dev.recica.scouter/.DashboardActivity
```

Configuration is accepted only from adb: the receiver requires
`android.permission.DUMP`, which other apps cannot hold, so no app on the
phone can redirect it to a server of its own and collect the token. A new
`url` is only accepted together with a `token`. Plain HTTP is allowed only
for `127.0.0.1`/`localhost`; everything else must be HTTPS.

For development against a local aggregator: `adb reverse tcp:8787 tcp:8787`
and configure `--es url http://127.0.0.1:8787` (with its token).

### Other phones?

Any Android 9+ phone runs the app. Two parts are specific to this one: the
status LED needs a phone that still has a notification LED (and a ROM that
honours notification light colours), and the charge limit below uses a
Samsung battery driver plus a `userdebug` LineageOS build.

### Battery

The phone is always on USB power. `/system/etc/init/scouter-charge.rc` (source: `android/device/scouter-charge.rc`; push it after `adb remount`) turns on
Samsung's retail *store mode* at boot, which holds the battery at about 60–70%
instead of 100% (full and warm is what makes old batteries swell). Delete that
file to restore normal charging.

## License

MIT, see [LICENSE](LICENSE).
