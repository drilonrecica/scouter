package icons

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/drilonrecica/scouter/aggregator/internal/github"
)

func files(paths ...string) []github.File {
	var out []github.File
	for _, p := range paths {
		out = append(out, github.File{Path: p, Size: 4000})
	}
	return out
}

func TestPickPrefersAnAppIconOverAStrayImage(t *testing.T) {
	for _, c := range []struct {
		name  string
		files []github.File
		want  string
	}{
		{"root logo beats a deep favicon", files("web/src/app/favicon.ico", "logo.png", "README.md"), "logo.png"},
		{"apple-touch-icon beats favicon", files("public/favicon.ico", "public/apple-touch-icon.png"), "public/apple-touch-icon.png"},
		{"android launcher, highest density", files("app/src/main/res/mipmap-hdpi/ic_launcher.png", "app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"), "app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"},
		{"branding folder counts", files("branding/icon-512.png", "branding/social-preview.png"), "branding/icon-512.png"},
		{"dependencies never count", files("node_modules/pkg/logo.png", "vendor/x/icon.png"), ""},
		{"svg cannot be rasterised", files("public/favicon.svg", "logo.svg"), ""},
		{"screenshots are not icons", files("docs/screenshot.png", "test/fixtures/logo.png"), ""},
		{"a bundled plugin's icon is not the project's", files("game/addons/gut/icon.png"), ""},
		{"sprite sheets merely mention icons", files("docs/art/ui/hint-icons.png", "docs/art/ui/frames/icons@2x.png"), ""},
		{"a named logo anywhere shallow", files("web/img/drejto-logo.png", "docs/art/ui/hint-icons.png"), "web/img/drejto-logo.png"},
		{"next.js app favicon", files("recica/public/favicon.ico", "recica/public/apple-touch-icon.png", "labs/static/favicon.svg"), "recica/public/apple-touch-icon.png"},
	} {
		if got := pick(c.files); got != c.want {
			t.Errorf("%s: pick = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPickSkipsHugeFiles(t *testing.T) {
	if got := pick([]github.File{{Path: "logo.png", Size: 5 << 20}}); got != "" {
		t.Errorf("pick = %q, want nothing for a 5 MB file", got)
	}
}

// square draws a solid w×h image with a transparent border of pad pixels.
func square(w, h, pad int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w+2*pad, h+2*pad))
	for y := pad; y < pad+h; y++ {
		for x := pad; x < pad+w; x++ {
			img.Set(x, y, color.NRGBA{200, 40, 40, 255})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decoded(t *testing.T, b []byte) image.Image {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestNormalizeTrimsAndScalesToSize(t *testing.T) {
	out, err := normalize(encodePNG(t, square(200, 100, 30)))
	if err != nil {
		t.Fatal(err)
	}
	img := decoded(t, out)
	if b := img.Bounds(); b.Dx() != Size || b.Dy() != Size {
		t.Fatalf("size %v, want %d×%d", b, Size, Size)
	}
	// 200×100 content, centred in a square: top quarter transparent, middle solid.
	if _, _, _, a := img.At(Size/2, 2).RGBA(); a != 0 {
		t.Error("padding should be transparent")
	}
	if _, _, _, a := img.At(Size/2, Size/2).RGBA(); a == 0 {
		t.Error("content should be opaque")
	}
	if _, _, _, a := img.At(0, Size/2).RGBA(); a == 0 {
		t.Error("transparent border should have been trimmed: content must reach the edge")
	}
}

func TestNormalizeReadsJPEGAndTinyFavicons(t *testing.T) {
	var j bytes.Buffer
	jpeg.Encode(&j, square(64, 64, 0), nil)
	for name, b := range map[string][]byte{"jpeg": j.Bytes(), "16px png": encodePNG(t, square(16, 16, 0))} {
		out, err := normalize(b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if decoded(t, out).Bounds().Dx() != Size {
			t.Errorf("%s: not scaled to %d", name, Size)
		}
	}
}

func TestNormalizeRejectsBlankAndUnknown(t *testing.T) {
	if _, err := normalize(encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 32, 32)))); err == nil {
		t.Error("fully transparent image should be rejected")
	}
	if _, err := normalize([]byte("<svg/>")); err == nil {
		t.Error("svg should be rejected")
	}
}

// ico wraps entries (raw image data, with their declared width) in an ICO container.
func ico(entries ...[]byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(entries))})
	offset := 6 + 16*len(entries)
	for i, e := range entries {
		w := byte(16 << i) // 16, 32, ...
		b.Write([]byte{w, w, 0, 0})
		binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(e)), uint32(offset)})
		offset += len(e)
	}
	for _, e := range entries {
		b.Write(e)
	}
	return b.Bytes()
}

// bmp32 is a 32-bit BMP as stored in ICO files: header, bottom-up BGRA rows, height doubled.
func bmp32(w, h int, c color.NRGBA) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, struct {
		Size          uint32
		Width, Height int32
		Planes, Bits  uint16
		Rest          [6]uint32
	}{Size: 40, Width: int32(w), Height: int32(2 * h), Planes: 1, Bits: 32})
	for i := 0; i < w*h; i++ {
		b.Write([]byte{c.B, c.G, c.R, c.A})
	}
	return b.Bytes()
}

func TestNormalizeReadsICOTakingTheLargestEntry(t *testing.T) {
	small := bmp32(16, 16, color.NRGBA{0, 0, 255, 255})
	large := encodePNG(t, square(32, 32, 0)) // red, as a PNG entry
	out, err := normalize(ico(small, large))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, b, _ := decoded(t, out).At(Size/2, Size/2).RGBA(); r>>8 < 150 || b>>8 > 100 {
		t.Errorf("took the wrong entry: r=%d b=%d", r>>8, b>>8)
	}
	out, err = normalize(ico(small))
	if err != nil {
		t.Fatalf("bmp entry: %v", err)
	}
	if _, _, b, _ := decoded(t, out).At(Size/2, Size/2).RGBA(); b>>8 < 200 {
		t.Error("bmp entry decoded wrong")
	}
}

func TestSiteIconLinks(t *testing.T) {
	page := `<html><head>
		<link rel="stylesheet" href="/app.css">
		<link rel="icon" type="image/svg+xml" href="/favicon.svg">
		<link rel="icon" sizes="32x32" href="/favicon-32.png">
		<LINK href="/touch.png" REL="apple-touch-icon" sizes="180x180">
		<link rel="shortcut icon" href="favicon.ico">
	</head></html>`
	got := iconLinks(page)
	want := []string{"/touch.png", "/favicon-32.png", "favicon.ico"}
	if len(got) != len(want) {
		t.Fatalf("links = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("links = %q, want %q", got, want)
		}
	}
}
