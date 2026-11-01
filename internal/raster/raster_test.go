package raster

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"snaplite/internal/annotate"
)

var red = annotate.Palette[0]

func gray(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 200, 200, 255
	}
	return img
}

func at(img *image.RGBA, x, y int) color.RGBA { return img.RGBAAt(x, y) }

func isRed(c color.RGBA) bool  { return c == color.RGBA{red.R, red.G, red.B, 255} }
func isBase(c color.RGBA) bool { return c == color.RGBA{200, 200, 200, 255} }

func TestComposeRectangle(t *testing.T) {
	base := gray(100, 100)
	out := Compose(base, []annotate.Shape{{Tool: annotate.ToolRect, Color: red, Width: 4, Points: []image.Point{{20, 20}, {80, 60}}}})
	if !isBase(at(base, 20, 20)) {
		t.Fatal("Compose must not modify its input")
	}
	// Pixel centres on the top edge (y=20 is the line, covering 18..22).
	if c := at(out, 50, 20); !isRed(c) {
		t.Fatalf("line centre = %v", c)
	}
	if c := at(out, 50, 40); !isBase(c) {
		t.Fatalf("inside the rectangle = %v", c)
	}
	if c := at(out, 50, 26); !isBase(c) {
		t.Fatalf("more than w outside the line = %v", c)
	}
	if c := at(out, 5, 5); !isBase(c) {
		t.Fatalf("far corner = %v", c)
	}
}

func TestComposeDiagonalIsAntialiased(t *testing.T) {
	base := gray(60, 60)
	out := Compose(base, []annotate.Shape{{Tool: annotate.ToolPen, Color: red, Width: 2, Points: []image.Point{{5, 5}, {55, 55}}}})
	mixed := false
	for x := 0; x < 60; x++ {
		c := at(out, x, 30)
		if !isRed(c) && !isBase(c) {
			mixed = true
		}
	}
	if !mixed {
		t.Fatal("a diagonal line has partially covered pixels")
	}
	if c := at(out, 30, 30); c.R < 220 {
		t.Fatalf("diagonal centre should be mostly red: %v", c)
	}
}

func TestComposeArrowAndEllipse(t *testing.T) {
	base := gray(200, 120)
	out := Compose(base, []annotate.Shape{
		{Tool: annotate.ToolArrow, Color: red, Width: 4, Points: []image.Point{{10, 20}, {110, 20}}},
		{Tool: annotate.ToolEllipse, Color: annotate.Palette[3], Width: 2, Points: []image.Point{{20, 40}, {180, 110}}},
	})
	// inside the solid head (16 long, 8 half wide) away from the shaft line
	if c := at(out, 100, 23); !isRed(c) {
		t.Fatalf("arrow head = %v", c)
	}
	if c := at(out, 50, 30); !isBase(c) {
		t.Fatalf("beside the shaft = %v", c)
	}
	blue := annotate.Palette[3]
	if c := at(out, 100, 40); c != (color.RGBA{blue.R, blue.G, blue.B, 255}) {
		t.Fatalf("ellipse top = %v", c)
	}
	if c := at(out, 100, 75); !isBase(c) {
		t.Fatalf("ellipse centre = %v", c)
	}
}

func TestComposeOffset(t *testing.T) {
	crop := gray(50, 50)
	out := ComposeOffset(crop, []annotate.Shape{{Tool: annotate.ToolRect, Color: red, Width: 2, Points: []image.Point{{110, 110}, {130, 130}}}}, image.Point{X: 100, Y: 100})
	if c := at(out, 20, 10); !isRed(c) {
		t.Fatalf("shape not shifted into the crop: %v", c)
	}
}

func TestToBGRAPremultiplies(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	copy(img.Pix, []byte{10, 20, 30, 255, 200, 100, 50, 128})
	out := ToBGRA(nil, img)
	want := []byte{30, 20, 10, 255, 25, 50, 100, 128}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("bgra = %v", out)
		}
	}
	reused := ToBGRA(out[:0], img)
	if &reused[0] != &out[0] {
		t.Fatal("dst buffer not reused")
	}
	bm := Bitmap(img)
	if bm.Width != 2 || bm.Stride != 8 || bm.ID == 0 || Bitmap(img).ID == bm.ID {
		t.Fatal("bitmap ids must be unique")
	}
}

func TestThumbnailAndCrop(t *testing.T) {
	img := gray(400, 100)
	th := Thumbnail(img, 96)
	if th.Bounds().Dx() != 96 || th.Bounds().Dy() != 24 || !isBase(at(th, 10, 10)) {
		t.Fatalf("thumbnail %v", th.Bounds())
	}
	small := Thumbnail(gray(10, 20), 96)
	if small.Bounds().Dx() != 10 {
		t.Fatal("small images keep their size")
	}
	c := Crop(img, image.Rect(390, 90, 500, 500))
	if c.Bounds() != image.Rect(0, 0, 10, 10) {
		t.Fatalf("crop is clipped to the image: %v", c.Bounds())
	}
	data, err := EncodePNG(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePNG(data)
	if err != nil || back.Bounds() != c.Bounds() || !isBase(at(back, 3, 3)) {
		t.Fatal("PNG round trip")
	}
}

func BenchmarkToBGRA4K(b *testing.B) {
	img := gray(3840, 2160)
	dst := make([]byte, 3840*2160*4)
	b.SetBytes(int64(len(dst)))
	b.ReportAllocs()
	for b.Loop() {
		dst = ToBGRA(dst, img)
	}
}

func TestEncodeFile(t *testing.T) {
	img := gray(64, 48)
	data, ext, err := EncodeFile(img, 0)
	if err != nil || ext != "png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("quality 0 is PNG: %q %v", ext, err)
	}
	sizes := map[int]int{}
	for _, q := range []int{92, 80, 60} {
		data, ext, err := EncodeFile(noise(256, 256), q)
		if err != nil || ext != "jpg" || !bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
			t.Fatalf("quality %d is JPEG: %q %v", q, ext, err)
		}
		back, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil || back.Bounds() != image.Rect(0, 0, 256, 256) {
			t.Fatalf("quality %d does not decode: %v", q, err)
		}
		sizes[q] = len(data)
	}
	if !(sizes[92] > sizes[80] && sizes[80] > sizes[60]) {
		t.Fatalf("lower quality should be smaller: %v", sizes)
	}
	// A flat grey survives JPEG almost unchanged.
	data, _, _ = EncodeFile(img, 80)
	back, _ := jpeg.Decode(bytes.NewReader(data))
	if r, _, _, _ := back.At(10, 10).RGBA(); r>>8 < 195 || r>>8 > 205 {
		t.Fatalf("JPEG grey = %d", r>>8)
	}
}

func TestEncodeJPEGPutsTransparencyOnWhite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16)) // fully transparent black
	data, err := EncodeJPEG(img, 92)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := jpeg.Decode(bytes.NewReader(data))
	if r, g, b, _ := back.At(8, 8).RGBA(); r>>8 < 245 || g>>8 < 245 || b>>8 < 245 {
		t.Fatalf("transparent pixels became %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
	if img.Pix[3] != 0 {
		t.Fatal("the picture itself is not changed")
	}
}

// noise is a picture JPEG cannot compress well, so quality shows in the size.
func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for i := 0; i < len(img.Pix); i += 4 {
		seed = seed*1664525 + 1013904223
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(seed>>24), uint8(seed>>16), uint8(seed>>8), 255
	}
	return img
}

func TestDownscaleAverages(t *testing.T) {
	// A 2× capture of a checkerboard of single physical pixels becomes a flat
	// mid grey at 1×: each logical pixel averages its 2×2 box.
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			v := uint8(0)
			if (x+y)%2 == 0 {
				v = 200
			}
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	out := Downscale(img, 2)
	if out.Bounds() != image.Rect(0, 0, 4, 3) {
		t.Fatalf("1× size = %v", out.Bounds())
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			if c := at(out, x, y); c != (color.RGBA{100, 100, 100, 255}) {
				t.Fatalf("pixel %d,%d = %v, want the 2×2 average", x, y, c)
			}
		}
	}
	// A fractional scale rounds the size; 1× and below leave the picture alone.
	if b := Downscale(gray(300, 200), 1.5).Bounds(); b != image.Rect(0, 0, 200, 133) {
		t.Fatalf("1.5× → %v", b)
	}
	if src := gray(30, 20); Downscale(src, 1) != src {
		t.Fatal("scale 1 returns the picture itself")
	}
	if b := Shrink(gray(10, 10), 40, 0).Bounds(); b != image.Rect(0, 0, 10, 1) {
		t.Fatalf("Shrink never enlarges and keeps at least one pixel: %v", b)
	}
}

func TestComposeMosaicUnderStrokes(t *testing.T) {
	base := gray(60, 60)
	grey := annotate.Color{R: 10, G: 20, B: 30, A: 255}
	mosaic := annotate.Shape{Tool: annotate.ToolMosaic, Block: 10, Cells: []annotate.Cell{{X: 10, Y: 10, Color: grey}}}
	pen := annotate.Shape{Tool: annotate.ToolPen, Color: red, Width: 4, Points: []image.Point{{0, 15}, {60, 15}}}
	// The pen comes first, yet the mosaic must not cover it.
	out := ComposeOffset(base, []annotate.Shape{pen, mosaic}, image.Point{})
	if c := at(out, 12, 12); c != (color.RGBA{10, 20, 30, 255}) {
		t.Fatalf("mosaic block: %v", c)
	}
	if !isRed(at(out, 15, 15)) {
		t.Fatalf("the pen is under the mosaic: %v", at(out, 15, 15))
	}
	if !isBase(at(out, 25, 25)) || !isBase(at(out, 5, 5)) {
		t.Fatal("the mosaic spills out of its block")
	}
	shifted := ComposeOffset(base, []annotate.Shape{mosaic}, image.Point{X: 5, Y: 5})
	if c := at(shifted, 5, 5); c != (color.RGBA{10, 20, 30, 255}) || !isBase(at(shifted, 4, 4)) {
		t.Fatal("the mosaic ignores the crop's origin")
	}
}
