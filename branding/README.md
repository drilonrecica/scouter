# Branding

The mark is the Scouter's lens: angled glass with one clipped corner and a
single status dot. The dot is the only colour that changes; it is white when
there is nothing to report, and takes the phone's status colours where the
mark shows live state (the admin favicon).

| File | Use |
|---|---|
| `mark.svg` | The mark on its own, 48 px and up |
| `favicon.svg` | Small cut for 16–32 px: thicker lens, larger dot, no readout lines |
| `logo.svg` | Mark and wordmark, transparent background |
| `header.svg` | README header, on a black plate so it reads the same in light and dark themes |
| `social-preview.png` | GitHub social preview, 1280×640 (Settings → Social preview); source in `social-preview.svg` |
| `icon-512.png` | The launcher icon at 512 px, for anywhere that wants a square image |

The launcher icon is the same drawing as `mark.svg`, written as a vector
drawable in `android/app/src/main/res/drawable/ic_launcher_*.xml`. The admin
favicon is the same drawing as `favicon.svg`, built in
`aggregator/internal/admin/favicon.go`. Change the three together.

## Colours

| | |
|---|---|
| Lens | `#45B5A0` outline, `#061412` glass |
| Dot, idle | `#E8E8E8` |
| Dot, live | the phone's status colours: green `#30D158`, amber `#FFB340`, red `#FF453A`, blue `#4DA3FF`, purple `#B06BFF` (no connection) |
| Icon background | `#05080B` |

## Type

The wordmark is `SCOUTER` in Share Tech Mono (Carrois Type Design, SIL Open
Font License 1.1), tracked +0.22 em. All text in these files is outlined, so
nothing depends on the font being installed.

## Rules

- Black ground. The mark is drawn for dark backgrounds; on white, use
  `header.svg` (it brings its own black plate).
- Only the dot carries status colour. Never recolour the lens.
- Inspired by the Scouter from Dragon Ball; not affiliated with or endorsed
  by Toei Animation or Shueisha. These are original drawings, not official
  artwork.
