package annotate

import (
	"image"
	"math"
)

// Cell is one mosaic block: its top-left corner in image pixels and the
// average colour of the original picture under it.
type Cell struct {
	X, Y  int
	Color Color
}

// mosaicSizes are the brush diameter and block side, in logical pixels, of
// the three widths: a mosaic brush is much wider than a pen.
var mosaicSizes = [len(Widths)]struct{ diameter, block float64 }{{16, 6}, {28, 9}, {48, 14}}

// MosaicSize is the brush diameter and block side in image pixels for a
// stroke width (one of Widths) on a picture of scale image pixels per
// logical pixel.
func MosaicSize(width float32, scale float64) (diameter float64, block int) {
	if scale <= 0 {
		scale = 1
	}
	size := mosaicSizes[1]
	for i, w := range Widths {
		if w == width {
			size = mosaicSizes[i]
		}
	}
	return size.diameter * scale, max(2, int(math.Round(size.block*scale)))
}

// mosaicBrush paints the cells of one mosaic stroke. Cells sit on a grid of
// block-sized squares anchored at the picture's origin, so overlapping
// strokes line up; a cell joins the stroke when its centre is within the
// brush radius of the stroke's path.
type mosaicBrush struct {
	src    *image.RGBA
	radius float64
	block  int
	seen   map[image.Point]bool
}

// stamp adds the cells along the segment from a to b to s.
func (m *mosaicBrush) stamp(s *Shape, a, b image.Point) {
	if m.src == nil {
		return
	}
	r := m.radius
	bounds := m.src.Bounds()
	bs := float64(m.block)
	gx0 := int(math.Floor((math.Min(float64(a.X), float64(b.X)) - r) / bs))
	gy0 := int(math.Floor((math.Min(float64(a.Y), float64(b.Y)) - r) / bs))
	gx1 := int(math.Floor((math.Max(float64(a.X), float64(b.X)) + r) / bs))
	gy1 := int(math.Floor((math.Max(float64(a.Y), float64(b.Y)) + r) / bs))
	for gy := gy0; gy <= gy1; gy++ {
		for gx := gx0; gx <= gx1; gx++ {
			key := image.Point{X: gx, Y: gy}
			if m.seen[key] {
				continue
			}
			cell := image.Rect(gx*m.block, gy*m.block, (gx+1)*m.block, (gy+1)*m.block).Intersect(bounds)
			if cell.Empty() {
				continue
			}
			c := FPoint{X: (float64(gx) + 0.5) * bs, Y: (float64(gy) + 0.5) * bs}
			if segmentDistance(c, fp(a), fp(b)) > r {
				continue
			}
			m.seen[key] = true
			s.Cells = append(s.Cells, Cell{X: gx * m.block, Y: gy * m.block, Color: average(m.src, cell)})
		}
	}
}

// segmentDistance is the distance from p to the segment ab.
func segmentDistance(p, a, b FPoint) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
	}
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// average is the mean colour of the picture over r, which lies inside it.
func average(img *image.RGBA, r image.Rectangle) Color {
	var sr, sg, sb, n uint32
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[img.PixOffset(r.Min.X, y):]
		for x := 0; x < r.Dx(); x++ {
			p := row[x*4 : x*4+4]
			sr, sg, sb = sr+uint32(p[0]), sg+uint32(p[1]), sb+uint32(p[2])
			n++
		}
	}
	if n == 0 {
		return Color{A: 0xFF}
	}
	return Color{R: uint8(sr / n), G: uint8(sg / n), B: uint8(sb / n), A: 0xFF}
}
