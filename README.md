<h1 align="center">
  <img src="branding/logo.svg" alt="Scouter" height="96">
</h1>

<p align="center"><em>"It's over 9000!"</em> (failed builds, hopefully not). Reads your CI's power level.</p>

<p align="center">
  <a href="https://github.com/drilonrecica/scouter/actions/workflows/aggregator.yml"><img src="https://github.com/drilonrecica/scouter/actions/workflows/aggregator.yml/badge.svg?branch=master" alt="Aggregator CI"></a>
  <a href="https://github.com/drilonrecica/scouter/actions/workflows/android.yml"><img src="https://github.com/drilonrecica/scouter/actions/workflows/android.yml/badge.svg?branch=master" alt="Android CI"></a>
  <a href="https://github.com/drilonrecica/scouter/tags"><img src="https://img.shields.io/github/v/tag/drilonrecica/scouter?label=version" alt="Latest version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/drilonrecica/scouter" alt="MIT license"></a>
</p>

<p align="center"><strong><a href="https://drilonrecica.github.io/scouter/">drilonrecica.github.io/scouter</a></strong>: what it is, how it looks, how to set it up.</p>

A retired Samsung Galaxy S3 Neo sits on the desk
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
- **`android/`** renders that document as a Scouter HUD. No dependencies, ~150 KB APK, about 26 MB RAM.

## What the phone shows

| | |
|---|---|
| **Focus** | One project large: CI status of the default branch, workflow, commit, duration, newer runs on other branches, open PRs (bots excluded), power level. Follows your latest push (`TRACKING`), a pinned project (`LOCKED`) or cycles your favorites (`ROTATING`). Tap for the next project (`MANUAL`); the next push hands Focus back. Long-press to pin/unpin. |
| **Grid** | The most relevant projects, 9 at a time: favorites first in your order, then failing, running, the rest. Drag up for more; it returns to the top after a minute. Power levels and the average. Tap a tile to pin it. |
| **Backdrop** | A stage behind the HUD, picked by the Focus project: Namek, the wasteland, the tournament ring, Kami's Lookout or the Tournament of Power, drawn in code as dim, softened silhouettes, with the sky tinted by the hour. Status weather on top: ki sparks while a build runs, a storm with a cracked ground on a fresh failure, static without a connection. When everything turns green, the seven Dragon Balls and the dragon take over for a minute. Plus the ki aura (edge glow in the LED's colour). Switch the stage off for the classic star field and hex lens mesh. Dim by design. |
| **Settings** | Long-press the top strip. Favorites, hidden repos, focus mode, screen hours, kiosk, backdrop, wave, briefing. |
| **Deploys** | With Coolify connected: each repo's latest deployment (LIVE / DEPLOYING / DEPLOY FAILED) beside its CI, a grid glyph, and `⚠ DEPLOYED WHILE CI RED` when Coolify shipped a commit whose CI failed (it deploys on push without waiting). |
| **Agents** | Third screen: your Claude Code sessions as WAITING / WORKING / DONE. A waiting session turns the LED blue and shows `◆ WAITING` in Focus. |
| **History** | 14-day power sparkline under PWR. |
| **Icons** | Each project's own icon beside its name in Focus and Grid, dimmed so status stays the brightest thing on screen. The aggregator looks for an icon file in the repo (`logo.png`, `apple-touch-icon.png`, a favicon, Android's `ic_launcher.png`…), else the favicon of the deployed site (Coolify URL or the repo's homepage); SVG-only icons are skipped. |
| **Briefing** | When the screen turns on in the morning: what broke, recovered and deployed overnight (30 s, tap to close). |
| **Wave** | At night, wave over the top of the phone to see the dashboard for 20 s. |
| **Alert** | A *new* failure on a default branch (less than 12 h old) cracks the lens: `POWER LEVEL DROPPING`, `PWR 9000 → 6750`. Takes over the screen for a minute, then stays as a red strip until tapped. Wakes the screen for 5 minutes when it is off (once per alert, also across restarts). |
| **Power level** | Build health, 0–9000: the share of the last 20 finished default-branch commits that passed (a commit passes when all its workflows do). `----` until there is a record. Every Grid project at 9000 earns an `IT'S OVER 9000!`. |
| **LED** (screen off) | red = undismissed alert · blue = an agent waits for you · amber = a build is running · green = all quiet · purple = no connection |

Swipe left/right to change screens. The screen is on during the schedule
(default Mon–Fri 09:00–19:00) and off otherwise, for burn-in and power.
The on time must be before the off time: overnight schedules (22:00–06:00)
are not supported.

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

## Settings

The aggregator owns all settings (`/data/settings.json`): hidden repos,
ordered favorites (always tracked, shown first), focus mode (latest, pinned,
rotate), screen hours, kiosk, backdrop layers and the alert window. Two
editors, one source of truth: the **admin UI** and the phone's **settings
screen** (which sends `PUT /v1/settings` with the phone token). Changes reach
the phone within seconds through the stream. Secrets are never settings:
they stay in environment variables.

### Admin UI

`https://<your-domain>/admin/`, enabled only when `SCOUTER_ADMIN_PASSWORD`
is set (at least 12 characters; otherwise `/admin` is a 404). Pages: status
(phones connected, GitHub rate limit, sources, which secrets are set),
projects, phone.

Server-rendered, no JavaScript. Server-side sessions in an `HttpOnly`,
`SameSite=Strict` cookie (12 h); a CSRF token plus an Origin check on every
form; logins rate-limited (5 failures per client, 10 overall per 10 minutes);
a CSP that forbids scripts. For a second lock, put Cloudflare Access in front
of `/admin*`.

## Aggregator

| Env var | |
|---|---|
| `SCOUTER_TOKEN` | required: bearer token the phone sends |
| `SCOUTER_GITHUB_TOKEN` | required: fine-grained PAT, read-only *Actions*, *Contents*, *Pull requests*, *Metadata* on all your repos |
| `SCOUTER_ADMIN_PASSWORD` | optional: enables the admin UI (min 12 chars) |
| `SCOUTER_COOLIFY_URL` / `SCOUTER_COOLIFY_TOKEN` | optional: deploy status (read-only API token, without *read:sensitive*) |
| `SCOUTER_HOOK_TOKEN` | optional: a token only for Claude Code hooks (the phone token also works) |
| `SCOUTER_RELEASE_TOKEN` | optional: lets `tools/release.sh` publish APKs over the air (`POST /v1/release`) |
| `SCOUTER_IGNORE_REPOS` | optional: `owner/repo` list hidden on top of the settings |
| `SCOUTER_ADDR` | default `:8080` |
| `SCOUTER_DATA_DIR` | default `/data`; holds `state.json` so restarts don't start empty |

Endpoints: `GET /v1/stream` (SSE, full state on every change, ping every 25 s),
`GET /v1/state` (with ETag), `PUT /v1/settings`, `POST /v1/heartbeat`,
`GET /v1/app.apk`, `GET /v1/icons/{hash}`, `POST /v1/agent-events`,
`GET`/`POST /v1/release` (release token), `GET /healthz`. The phone falls
back to polling `/v1/state` when a proxy buffers the stream, and retries the
stream every 10 minutes.

The document carries a `schema` number. It goes up only for changes an older
app would misread; the phone then shows `UPDATE APP` instead of guessing.

**Resilience.** A repo that fails (renamed, access revoked) backs off on its
own and is noted on the admin status page without holding up the others; a
GitHub rate limit pauses polling until the reset time GitHub sends. Files in
`/data` are written atomically and flushed to disk. A file that cannot be
read at startup is kept as `<name>.bad-<time>` (never overwritten) and listed
on the admin status page, and so is any file that fails to save.

**Coolify:** new resource → this repo, build pack *Dockerfile*, base directory
`/aggregator`, port 8080, a persistent volume on `/data`, the env vars above as
secrets. Leave Coolify's own health check **off**: it runs `curl` through
`/bin/sh` inside the container, and the image is distroless (no shell, no
`curl`/`wget`), so it fails every deploy. The image checks itself instead:
its Dockerfile `HEALTHCHECK` runs `/scouter-aggregator healthcheck`, which
asks `/healthz`, so Coolify shows the container as healthy or unhealthy.
Turning Coolify's check on replaces that one.

`/healthz` answers `ok <commit>`, or `503` with the reasons when a file in
`/data` cannot be saved or the GitHub poller has stalled (a GitHub outage
or rate limit does not count). The commit comes from Coolify's
`SOURCE_COMMIT` build argument (enable *Include Source Commit in Build*)
and also shows on the admin status page.

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
adb shell dumpsys deviceidle whitelist +dev.recica.scouter          # Doze would stall the schedule overnight

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

### Kiosk mode

Needs Scouter as **device owner**, which Android only allows on a phone with
no accounts (Settings → Accounts empty):

```sh
adb shell dpm set-device-owner dev.recica.scouter/.AdminReceiver
```

Then switch *Kiosk* on in either settings editor: lock task (Home, Recents and
the notification shade do nothing), no status bar, no lock screen. The screen
schedule still applies: kiosk keeps people in the app, it does not keep the
AMOLED lit all night. Leave kiosk from the phone's settings screen or the
admin UI.

Before uninstalling, release device ownership: settings screen → *Release
device owner*, or if the phone is unreachable:

```sh
adb shell am broadcast -n dev.recica.scouter/.ConfigReceiver --ez release_owner true
```

### Over-the-air updates

With Scouter as device owner, upload a newer APK (same signing key, higher
`versionCode`) on the admin **App** page. The phone downloads it, checks its
sha256, installs it silently and restarts itself. Each upload is installed at
most once; a download that fails is retried every 5 minutes. The result, and
the app's last crash if any, show on the admin status page.

Or from the build machine in one go: `tools/release.sh` bumps the version,
builds, publishes with `SCOUTER_RELEASE_TOKEN` (a token that can only publish
APKs; locally in `~/.config/scouter/release-token`), commits the version bump,
tags it `v<version>` and waits until the phone reports the new version. It
refuses to run with uncommitted changes under `android/`, so each tag is
exactly the code on the phone. Push the tags with `git push --follow-tags`.

**Back up the signing key.** Releases are signed with the build machine's
debug key (`~/.android/debug.keystore`). Android only installs an update
signed with the same key, so if that file is lost, over-the-air updates stop
and the app has to be reinstalled over adb (release device ownership first).
Keep a copy of it somewhere safe.

### Claude Code agents

`~/.config/scouter/hook.sh` (copy of `tools/claude-hook.sh`) reports session state to the aggregator. It
forwards only `session_id`, `cwd` and the event name (never prompts or
transcripts), runs in the background and always exits 0. Register it for
`UserPromptSubmit`, `Notification`, `Stop` and `SessionEnd` in
`~/.claude/settings.json`; put the URL in `~/.config/scouter/url` and
`Authorization: Bearer <token>` in `~/.config/scouter/auth-header` (0600).

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
