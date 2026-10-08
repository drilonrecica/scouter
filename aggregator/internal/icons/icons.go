// Package icons finds each project's icon (an image in its repo, else its
// deployed site's favicon), normalizes it to a small PNG and publishes its
// hash; the phone fetches the bytes by hash from the server.
package icons

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // registers the decoder
	_ "image/jpeg"
	"image/png"
	"path"
	"regexp"
	"strings"

	"github.com/drilonrecica/scouter/aggregator/internal/github"
)

// Size is the edge of every normalized icon, in pixels: enough for the
// phone's largest use (30dp at xhdpi) with room to spare.
const Size = 96

const (
	maxFile      = 1 << 20   // never download a bigger candidate
	maxLooseFile = 200 << 10 // "*logo*"-style guesses must be small, like icons are
)

var rasters = map[string]bool{".png": true, ".ico": true, ".jpg": true, ".jpeg": true, ".gif": true}

// Directories whose images are someone else's or not icons at all.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "third_party": true, "pods": true, ".git": true,
	"test": true, "tests": true, "testdata": true, "fixtures": true, "__tests__": true,
	"examples": true, "example": true, "build": true, "dist": true, "out": true, "target": true,
	"addons": true, // Godot's folder for third-party plugins
}

// namedIcon matches file stems that are a logo or icon, not ones that merely
// mention one ("hint-icons", "icons@2x"): "logo", "app-icon", "icon_512", "drejto-logo".
var namedIcon = regexp.MustCompile(`^((app|site)[-_]?)?(logo|icon)([-_]\w+)?$|^[\w.]+[-_](logo|icon)$`)

// Directories where a project keeps its own logo.
var logoDirs = map[string]bool{
	".": true, ".github": true, "assets": true, "public": true, "static": true,
	"branding": true, "brand": true, "images": true, "img": true, "docs": true, "media": true,
}

// pick chooses the repo file most likely to be the project's icon, or "".
func pick(files []github.File) string {
	best, bestScore := "", 0
	for _, f := range files {
		if s := score(f); s > bestScore {
			best, bestScore = f.Path, s
		}
	}
	return best
}

func score(f github.File) int {
	p := strings.ToLower(f.Path)
	if !rasters[path.Ext(p)] || f.Size > maxFile {
		return 0
	}
	dir, base := path.Dir(p), path.Base(p)
	segs := strings.Split(dir, "/")
	for _, s := range segs {
		if skipDirs[s] {
			return 0
		}
	}
	depth := len(segs)
	if dir == "." {
		depth = 0
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	var s int
	switch {
	case strings.HasPrefix(stem, "apple-touch-icon"):
		s = 90
	case (stem == "logo" || stem == "icon" || strings.HasPrefix(stem, "icon-") || stem == "app-icon") && logoDirs[segs[0]]:
		s = 80
	case stem == "ic_launcher" && strings.Contains(dir, "mipmap-xxxhdpi"):
		s = 75
	case stem == "ic_launcher" && strings.Contains(dir, "mipmap-xxhdpi"):
		s = 70
	case stem == "ic_launcher" && strings.Contains(dir, "mipmap-"):
		s = 55
	case strings.HasPrefix(stem, "favicon"):
		s = 60
		if path.Ext(base) == ".png" {
			s += 5 // usually larger than the .ico next to it
		}
	case namedIcon.MatchString(stem) && depth <= 3 && f.Size <= maxLooseFile:
		s = 30
	default:
		return 0
	}
	return s - depth // the shallower of two equals is the project's own
}

// normalize decodes an icon (PNG, JPEG, GIF or ICO), trims its transparent
// border, centres it on a transparent square and scales it to Size×Size PNG.
func normalize(b []byte) ([]byte, error) {
	var img image.Image
	var err error
	if bytes.HasPrefix(b, []byte{0, 0, 1, 0}) {
		img, err = decodeICO(b)
	} else {
		img, _, err = image.Decode(bytes.NewReader(b))
	}
	if err != nil {
		return nil, err
	}
	src := toNRGBA(img)
	content := opaqueBounds(src)
	if content.Empty() {
		return nil, errors.New("icon is fully transparent")
	}
	out := scale(src, content)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	n := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n.Set(x, y, img.At(x, y))
		}
	}
	return n
}

// opaqueBounds is the smallest rectangle holding every visible pixel.
func opaqueBounds(img *image.NRGBA) image.Rectangle {
	r := image.Rectangle{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A > 8 {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}

// scale fits the content rectangle of src, centred, into a Size×Size image:
// area-averaging (with premultiplied alpha) when shrinking, nearest pixel
// when enlarging a tiny favicon.
func scale(src *image.NRGBA, content image.Rectangle) *image.NRGBA {
	side := max(content.Dx(), content.Dy())
	// The square around the content, in source coordinates.
	ox := content.Min.X - (side-content.Dx())/2
	oy := content.Min.Y - (side-content.Dy())/2
	k := float64(side) / Size
	dst := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	at := func(x, y int) color.NRGBA {
		if !(image.Point{x, y}.In(content)) {
			return color.NRGBA{}
		}
		return src.NRGBAAt(x, y)
	}
	for dy := 0; dy < Size; dy++ {
		y0, y1 := oy+int(float64(dy)*k), oy+int(float64(dy+1)*k)
		for dx := 0; dx < Size; dx++ {
			x0, x1 := ox+int(float64(dx)*k), ox+int(float64(dx+1)*k)
			if x1 <= x0 || y1 <= y0 { // enlarging: one source pixel covers several
				dst.SetNRGBA(dx, dy, at(x0, y0))
				continue
			}
			var r, g, b, a, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := at(x, y)
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					b += uint64(c.B) * uint64(c.A)
					a += uint64(c.A)
					n++
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / n)})
		}
	}
	return dst
}

// decodeICO returns the largest image in an ICO file. Entries are PNG or a
// 32-bit BMP (the only BMP depth modern favicons use); others are rejected.
func decodeICO(b []byte) (image.Image, error) {
	if len(b) < 6 {
		return nil, errors.New("ico: truncated")
	}
	count := int(binary.LittleEndian.Uint16(b[4:]))
	var data []byte
	bestW := -1
	for i := 0; i < count; i++ {
		e := 6 + 16*i
		if e+16 > len(b) {
			return nil, errors.New("ico: truncated directory")
		}
		w := int(b[e])
		if w == 0 {
			w = 256
		}
		size, off := binary.LittleEndian.Uint32(b[e+8:]), binary.LittleEndian.Uint32(b[e+12:])
		if uint64(off)+uint64(size) > uint64(len(b)) {
			continue
		}
		if w > bestW {
			bestW, data = w, b[off:off+size]
		}
	}
	if data == nil {
		return nil, errors.New("ico: no usable entry")
	}
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return png.Decode(bytes.NewReader(data))
	}
	return decodeBMP32(data)
}

func decodeBMP32(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, errors.New("bmp: truncated")
	}
	hdr := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2 // ICO stores image + AND mask height
	if bits := binary.LittleEndian.Uint16(b[14:]); bits != 32 {
		return nil, errors.New("bmp: only 32-bit entries are supported")
	}
	if w <= 0 || h <= 0 || w > 512 || h > 512 || hdr+w*h*4 > len(b) {
		return nil, errors.New("bmp: bad dimensions")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	px := b[hdr:]
	for y := 0; y < h; y++ {
		row := px[(h-1-y)*w*4:] // bottom-up
		for x := 0; x < w; x++ {
			p := row[x*4:]
			img.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: p[3]})
		}
	}
	return img, nil
}
