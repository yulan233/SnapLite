package longshot

import (
	"bytes"
	"image"
	"math/rand"
	"testing"
)

const (
	pageW      = 48
	pageH      = 3000
	headerH    = 60
	footerH    = 40
	viewH      = 500
	contentH   = pageH - headerH - footerH
	viewport   = viewH - headerH - footerH
	whiteStart = 1200
	whiteEnd   = 1400
)

// longPage is a page whose every row differs, with a fixed header and footer
// and a 200-row white band in the middle of the content.
func longPage() *image.RGBA { return makePage(true) }

func makePage(withWhite bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	rng := rand.New(rand.NewSource(1))
	for y := 0; y < pageH; y++ {
		white := withWhite && y >= headerH+whiteStart && y < headerH+whiteEnd
		for x := 0; x < pageW; x++ {
			p := img.Pix[img.PixOffset(x, y):]
			if white {
				p[0], p[1], p[2] = 255, 255, 255
			} else {
				p[0], p[1], p[2] = uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256))
			}
			p[3] = 255
		}
	}
	return img
}

// view is what the scrolling region shows with the content scrolled by pos.
func view(page *image.RGBA, pos int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, pageW, viewH))
	copyRows := func(dst, src, n int) {
		copy(img.Pix[dst*img.Stride:(dst+n)*img.Stride], page.Pix[src*page.Stride:(src+n)*page.Stride])
	}
	copyRows(0, 0, headerH)
	copyRows(headerH, headerH+pos, viewport)
	copyRows(headerH+viewport, pageH-footerH, footerH)
	return img
}

func TestStitchReproducesThePage(t *testing.T) {
	page := longPage()
	rng := rand.New(rand.NewSource(7))
	var s Stitcher
	if r := s.Add(view(page, 0)); r.Kind != First {
		t.Fatalf("first = %v", r.Kind)
	}
	pos := 0
	for pos < contentH-viewport {
		step := 40 + rng.Intn(140)
		pos = min(pos+step, contentH-viewport)
		r := s.Add(view(page, pos))
		if r.Kind != Appended {
			t.Fatalf("at %d: %v", pos, r.Kind)
		}
	}
	got := s.Result()
	if got.Bounds() != page.Bounds() {
		t.Fatalf("result %v, page %v", got.Bounds(), page.Bounds())
	}
	if !bytes.Equal(got.Pix, page.Pix) {
		for y := 0; y < pageH; y++ {
			if !bytes.Equal(got.Pix[y*got.Stride:(y+1)*got.Stride], page.Pix[y*page.Stride:(y+1)*page.Stride]) {
				t.Fatalf("first differing row %d", y)
			}
		}
	}
}

func TestStitchNoChangeAndNoOverlap(t *testing.T) {
	page := longPage()
	var s Stitcher
	s.Add(view(page, 0))
	if r := s.Add(view(page, 0)); r.Kind != NoChange {
		t.Fatalf("same frame = %v", r.Kind)
	}
	if r := s.Add(view(page, viewport+50)); r.Kind != NoOverlap {
		t.Fatalf("a step beyond the viewport = %v", r.Kind)
	}
	if s.Height() != viewH {
		t.Fatal("rejected frames add nothing")
	}
	if r := s.Add(view(page, 100)); r.Kind != Appended || r.Rows != 100 {
		t.Fatalf("after a rejection the next good frame appends: %+v", r)
	}
	if r := s.Add(image.NewRGBA(image.Rect(0, 0, 10, 10))); r.Kind != NoOverlap {
		t.Fatal("a frame of another size is rejected")
	}
}

func BenchmarkStitchAdd(b *testing.B) {
	page := longPage()
	frames := []*image.RGBA{view(page, 0), view(page, 150)}
	b.ReportAllocs()
	for b.Loop() {
		var s Stitcher
		s.Add(frames[0])
		s.Add(frames[1])
	}
}

// BenchmarkStitchRetina adds one 1600×1800 frame (a 800×900-point region at
// 2×) scrolled by 700 rows: signatures plus the offset search.
func BenchmarkStitchRetina(b *testing.B) {
	const w, h, total = 1600, 1800, 2600
	page := image.NewRGBA(image.Rect(0, 0, w, total))
	rng := rand.New(rand.NewSource(3))
	rng.Read(page.Pix)
	frame := func(top int) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, page.Pix[top*page.Stride:(top+h)*page.Stride])
		return img
	}
	a, c := frame(0), frame(700)
	b.ReportAllocs()
	for b.Loop() {
		var s Stitcher
		s.Add(a)
		s.Add(c)
	}
}
