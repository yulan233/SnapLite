package annotate

import (
	"image"
	"testing"
)

// stripes is a picture of 1-pixel black and white columns, which a mosaic
// block averages to grey.
func stripes(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(0)
			if x%2 == 1 {
				v = 255
			}
			p := img.Pix[img.PixOffset(x, y):]
			p[0], p[1], p[2], p[3] = v, v, v, 255
		}
	}
	return img
}

func TestMosaicStroke(t *testing.T) {
	d := NewDocument()
	d.Source, d.Scale = stripes(200, 100), 1
	if !draw(&d, ToolMosaic, pt(20, 50), pt(180, 50), false) {
		t.Fatal("mosaic not committed")
	}
	s := d.Shapes[0]
	_, block := MosaicSize(d.Width, 1)
	if s.Block != block || len(s.Cells) == 0 {
		t.Fatalf("block %d, %d cells", s.Block, len(s.Cells))
	}
	seen := map[image.Point]bool{}
	for _, c := range s.Cells {
		if c.X%block != 0 || c.Y%block != 0 {
			t.Fatalf("cell %v is off the grid", c)
		}
		key := image.Point{X: c.X, Y: c.Y}
		if seen[key] {
			t.Fatalf("cell %v twice", c)
		}
		seen[key] = true
		if c.Color.R < 100 || c.Color.R > 155 || c.Color.A != 255 {
			t.Fatalf("cell %v is not the average grey", c)
		}
	}
	if !seen[image.Point{X: 99 / block * block, Y: 50 / block * block}] {
		t.Fatal("the middle of the stroke is not covered")
	}
	if seen[image.Point{X: 99 / block * block, Y: 0}] {
		t.Fatal("cells far from the stroke are covered")
	}
}

func TestMosaicDabIsKept(t *testing.T) {
	d := NewDocument()
	d.Source = stripes(50, 50)
	d.Tool = ToolMosaic
	d.Begin(pt(25, 25))
	if !d.Commit() || len(d.Shapes[0].Cells) == 0 {
		t.Fatal("a click with the mosaic brush should leave a dab")
	}
	snap := d.Snapshot()
	snap[0].Cells[0].X = -1
	if d.Shapes[0].Cells[0].X == -1 {
		t.Fatal("Snapshot shares the cells")
	}
}

func TestMosaicScalesWithDisplay(t *testing.T) {
	d1, b1 := MosaicSize(Widths[1], 1)
	d2, b2 := MosaicSize(Widths[1], 2)
	if d2 != 2*d1 || b2 != 2*b1 {
		t.Fatalf("1×: %v/%d, 2×: %v/%d", d1, b1, d2, b2)
	}
}
