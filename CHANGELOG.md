# Changelog

All notable changes to Scouter are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) and name the
phone app's `versionName`, which the `v<version>` tag must equal. Before
0.5.0 the aggregator was deployed from `master` and the app only over the air,
so the tags carried no release assets.

## [Unreleased]

## [0.5.0] - 2026-11-25

The first version with downloadable assets: a GitHub Release per tag with the
aggregator binaries and the phone APK.

### Added

- **Release workflow.** A `v*` tag builds the aggregator for linux and darwin,
  amd64 and arm64, with GoReleaser (`.goreleaser.yaml`, archives with
  `LICENSE`, `README.md` and `example.env`), builds the APK, and attaches
  everything with one `checksums.txt` to a GitHub Release with generated
  notes. The workflow needs no secrets; it refuses a tag that does not match
  `versionName`.
- **Signing from the environment.** `SCOUTER_KEYSTORE_FILE`,
  `SCOUTER_KEYSTORE_PASSWORD`, `SCOUTER_KEY_ALIAS` and `SCOUTER_KEY_PASSWORD`
  make Gradle sign the release build with that keystore; unset, it keeps
  using the debug key. The release workflow uses them when the matching
  repository secrets exist (`SCOUTER_KEYSTORE_BASE64` for the file), and
  names the asset `scouter-app_<version>_debug-signed.apk` otherwise.
- **`aggregator/example.env`**: every environment variable with a comment,
  shipped inside the release archives.
- **`docs/INSTALL.md`**: the full phone install, the one-time adb setup line
  by line, local development against the aggregator, and what the debug
  signature means.

### Changed

- **README** opens with *What you get* and a four-step *Quick start*
  (download, `adb install`, aggregator with `example.env`, point the app at
  it); the long adb list moved to `docs/INSTALL.md`. The aggregator section
  explains the binary, Docker and Coolify ways to run it, and a *Releases*
  section describes the tag flow.
- The aggregator binary's `version` (shown by `/healthz` and the admin
  status page) is the release version in GoReleaser builds; Docker builds
  still stamp the commit.

## [0.4.1] - 2026-10-09

### Fixed

- **Phone: the schedule stays on time through Doze.** Doze ignores the
  stream service's wake lock, so the 30 s schedule tick could stall
  overnight and the screen came on late. The settings screen shows whether
  battery optimization is still on and can ask Android to turn it off; the
  setup gained `adb shell dumpsys deviceidle whitelist +dev.recica.scouter`.

### Changed

- README: centred logo header with CI, version and license badges.

## [0.4.0] - 2026-10-08

The 1.0-readiness release of both halves: the aggregator survives bad repos,
rate limits, bad files and restarts, and the phone survives bad data and its
own updates.

### Added

- **Project page** on GitHub Pages (`site/`, no build step; deployed by
  `pages.yml`): a live phone in the hero drawing the real stages, weather
  and Focus HUD, plus how it works, the screens, the status light and setup.
- **`/healthz`** reports the build commit and real problems only (a storage
  file that cannot be saved, a stalled GitHub poller); the admin status
  page shows the build, storage problems and the phone's last crash.
- **Docker `HEALTHCHECK` without a shell:** `scouter-aggregator healthcheck`
  asks `/healthz` on loopback, so the distroless image reports its health
  to Coolify while Coolify's own check stays off.
- **Schema number** in the document: the phone shows `UPDATE APP` for a
  newer schema instead of guessing.
- Phone: the last crash travels with the heartbeat; alerted alerts and the
  screen-off time survive restarts.

### Changed

- **Aggregator resilience.** A failing repo (renamed, access revoked) backs
  off on its own without holding up the others; a GitHub rate limit pauses
  polling until the reset time. Unreadable data files are kept as
  `<name>.bad-<time>`, writes are atomic and fsynced, and load/save problems
  show on the admin page and in `/healthz`. Graceful shutdown ends open
  streams and waits for the pollers; stream writes have a deadline. The
  GitHub cache forgets dropped repos and skips huge answers.
- **Phone stream and parser.** One stream thread per connection attempt (no
  duplicates after a restart); a drop after data reconnects at once. The
  parser skips unreadable items instead of rejecting the whole document.
  Saving settings keeps the fields only the server knows.
- **Over-the-air updates** retry a failed download every 5 minutes and
  install each upload at most once. `tools/release.sh` commits and tags each
  release and refuses a dirty `android/`.
- Immersive mode works on Android 9 and 10; lint errors fail the build and
  CI runs lint.
- Dockerfile pins the base images by digest and stamps `SOURCE_COMMIT`; the
  admin login limit uses the proxy-appended `X-Forwarded-For` hop.

## 0.3.9 and earlier

Released over the air only, without tags. From the history: tap for the next
project (0.3.9), Dragon Ball-inspired stages with status
weather (0.3.8), project icons (0.3.7), the scrollable grid (0.3.6), the
lens-and-light launcher icon (0.3.5), over-the-air releases from
`tools/release.sh` (0.3.4), kiosk mode, deploy status from Coolify, Claude
Code agent sessions, power history, wave and briefing (0.3.x), the settings
screen and admin UI (0.2.0), and the first HUD with power levels.

[Unreleased]: https://github.com/drilonrecica/scouter/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/drilonrecica/scouter/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/drilonrecica/scouter/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/drilonrecica/scouter/releases/tag/v0.4.0
