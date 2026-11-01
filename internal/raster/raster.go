package raster

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"sync/atomic"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
)

// ToBGRA converts straight-alpha RGBA to the premultiplied BGRA ui.Bitmap
// wants, reusing dst when it is large enough.
func ToBGRA(dst []byte, src *image.RGBA) []byte {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	n := w * h * 4
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	dst = dst[:n]
	for y := 0; y < h; y++ {
		in := src.Pix[(y)*src.Stride : y*src.Stride+w*4]
		out := dst[y*w*4 : (y+1)*w*4]
		for x := 0; x+4 <= len(in); x += 4 {
			v := binary.LittleEndian.Uint32(in[x:])
			if v>>24 == 0xFF {
				// Opaque (every screenshot pixel): swap R and B in one word.
				binary.LittleEndian.PutUint32(out[x:], v&0xFF00FF00|(v&0xFF)<<16|(v>>16)&0xFF)
				continue
			}
			r, g, bl, a := uint32(in[x]), uint32(in[x+1]), uint32(in[x+2]), uint32(in[x+3])
			out[x], out[x+1], out[x+2], out[x+3] = uint8(bl*a/255), uint8(g*a/255), uint8(r*a/255), uint8(a)
		}
	}
	return dst
}

var bitmapIDs atomic.Uint64

// NewBitmapID returns a process-unique ui.Bitmap identity.
func NewBitmapID() uint64 { return bitmapIDs.Add(1) }

// Bitmap converts img into a new immutable ui.Bitmap with a fresh ID.
func Bitmap(img *image.RGBA) vui.Bitmap {
	b := img.Bounds()
	return vui.Bitmap{Pix: ToBGRA(nil, img), Width: b.Dx(), Height: b.Dy(), Stride: b.Dx() * 4, ID: NewBitmapID(), Version: 1}
}

// ToRGBA returns img as an *image.RGBA whose bounds start at the origin,
// converting when needed.
func ToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// Crop copies a rectangle of img into a new image at the origin.
func Crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(img.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		src := img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):]
		copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], src[:r.Dx()*4])
	}
	return out
}

// EncodePNG encodes quickly: screenshots are large and compress well anyway.
func EncodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodePNG decodes a PNG into *image.RGBA.
func DecodePNG(data []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return ToRGBA(img), nil
}

// EncodeJPEG encodes at quality 1–100. JPEG has no alpha: a picture with
// transparent pixels (a copied PNG; screenshots are opaque) is put on white.
func EncodeJPEG(img *image.RGBA, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, onWhite(img), &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// onWhite returns img when it is opaque, else a copy composited onto white.
func onWhite(img *image.RGBA) *image.RGBA {
	opaque := true
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xFF {
			opaque = false
			break
		}
	}
	if opaque {
		return img
	}
	out := Crop(img, img.Bounds())
	for i := 0; i < len(out.Pix); i += 4 {
		a := uint32(out.Pix[i+3])
		for c := 0; c < 3; c++ {
			out.Pix[i+c] = uint8((uint32(out.Pix[i+c])*a + 255*(255-a) + 127) / 255)
		}
		out.Pix[i+3] = 0xFF
	}
	return out
}

// EncodeFile encodes a picture for saving: JPEG at jpegQuality, or PNG when it
// is 0. It returns the data and the file extension ("png" or "jpg").
func EncodeFile(img *image.RGBA, jpegQuality int) ([]byte, string, error) {
	if jpegQuality <= 0 {
		data, err := EncodePNG(img)
		return data, "png", err
	}
	data, err := EncodeJPEG(img, jpegQuality)
	return data, "jpg", err
}

// Thumbnail shrinks img so its longer side is at most maxSide, averaging
// boxes of source pixels. Smaller images are copied unchanged.
func Thumbnail(img image.Image, maxSide int) *image.RGBA {
	src := ToRGBA(img)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= maxSide && h <= maxSide || w == 0 || h == 0 {
		return Crop(src, src.Bounds())
	}
	scale := float64(maxSide) / float64(max(w, h))
	return Shrink(src, max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale))))
}

// Downscale shrinks a capture by a display's scale (physical pixels per
// logical pixel) to one pixel per logical pixel. A scale of 1 or less returns
// img itself.
func Downscale(img *image.RGBA, scale float64) *image.RGBA {
	if scale <= 1 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	return Shrink(img, max(1, int(math.Round(float64(w)/scale))), max(1, int(math.Round(float64(h)/scale))))
}

// Shrink resamples src to tw×th, no larger than src, averaging the box of
// source pixels under each output pixel.
func Shrink(src *image.RGBA, tw, th int) *image.RGBA {
	src = ToRGBA(src)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	tw, th = min(max(tw, 1), max(w, 1)), min(max(th, 1), max(h, 1))
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	if w == 0 || h == 0 {
		return out
	}
	for ty := 0; ty < th; ty++ {
		y0, y1 := ty*h/th, max((ty+1)*h/th, ty*h/th+1)
		for tx := 0; tx < tw; tx++ {
			x0, x1 := tx*w/tw, max((tx+1)*w/tw, tx*w/tw+1)
			var r, g, b, a, n uint32
			for y := y0; y < y1; y++ {
				row := src.Pix[y*src.Stride:]
				for x := x0; x < x1; x++ {
					p := row[x*4 : x*4+4]
					r, g, b, a = r+uint32(p[0]), g+uint32(p[1]), b+uint32(p[2]), a+uint32(p[3])
					n++
				}
			}
			o := out.Pix[ty*out.Stride+tx*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)
		}
	}
	return out
}

// Compose copies base and paints the shapes onto it, anti-aliased.
func Compose(base *image.RGBA, shapes []annotate.Shape) *image.RGBA {
	out := Crop(base, base.Bounds())
	paintShapes(out, shapes, base.Bounds().Min)
	return out
}

// ComposeOffset paints shapes whose image coordinates are relative to a
// larger image onto a crop of it whose top-left corner was at origin.
func ComposeOffset(crop *image.RGBA, shapes []annotate.Shape, origin image.Point) *image.RGBA {
	out := Crop(crop, crop.Bounds())
	paintShapes(out, shapes, origin)
	return out
}

// paintShapes paints the mosaics first, under every other shape, as the
// overlay draws them.
func paintShapes(img *image.RGBA, shapes []annotate.Shape, origin image.Point) {
	for _, s := range shapes {
		if s.Tool == annotate.ToolMosaic {
			paintMosaic(img, s, origin)
		}
	}
	for _, s := range shapes {
		if s.Tool != annotate.ToolMosaic {
			paintShape(img, s, origin)
		}
	}
}

// paintMosaic fills a mosaic's blocks with their colours.
func paintMosaic(img *image.RGBA, s annotate.Shape, origin image.Point) {
	for _, c := range s.Cells {
		r := image.Rect(c.X, c.Y, c.X+s.Block, c.Y+s.Block).Sub(origin).Intersect(img.Bounds())
		col := color.RGBA{R: c.Color.R, G: c.Color.G, B: c.Color.B, A: c.Color.A}
		draw.Draw(img, r, image.NewUniform(col), image.Point{}, draw.Src)
	}
}

// mask is the coverage of one shape over a rectangle of the image.
type mask struct {
	rect image.Rectangle
	c    []float32
}

func (m *mask) set(x, y int, v float32) {
	i := (y-m.rect.Min.Y)*m.rect.Dx() + (x - m.rect.Min.X)
	if v > m.c[i] {
		m.c[i] = v
	}
}

func paintShape(img *image.RGBA, s annotate.Shape, origin image.Point) {
	if len(s.Points) == 0 {
		return
	}
	w := float64(s.Width)
	if w <= 0 {
		w = 1
	}
	var lines [][]annotate.FPoint
	var tri *[3]annotate.FPoint
	switch s.Tool {
	case annotate.ToolRect:
		lines = append(lines, annotate.RectPolyline(s.Points[0], s.Points[1]))
	case annotate.ToolEllipse:
		lines = append(lines, annotate.EllipsePolyline(s.Points[0], s.Points[1]))
	case annotate.ToolPen:
		lines = append(lines, annotate.PenPolyline(s.Points))
	case annotate.ToolArrow:
		g := annotate.Arrow(s.Points[0], s.Points[1], w)
		lines = append(lines, []annotate.FPoint{g.Start, g.ShaftEnd})
		tri = &g.Head
	default:
		return
	}
	ox, oy := float64(origin.X), float64(origin.Y)
	// shift into the output image's coordinates
	for _, line := range lines {
		for i := range line {
			line[i].X -= ox
			line[i].Y -= oy
		}
	}
	if tri != nil {
		for i := range tri {
			tri[i].X -= ox
			tri[i].Y -= oy
		}
	}
	pad := w/2 + 2
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	extend := func(p annotate.FPoint) {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	for _, line := range lines {
		for _, p := range line {
			extend(p)
		}
	}
	if tri != nil {
		for _, p := range tri {
			extend(p)
		}
	}
	box := image.Rect(int(math.Floor(minX-pad)), int(math.Floor(minY-pad)), int(math.Ceil(maxX+pad))+1, int(math.Ceil(maxY+pad))+1).Intersect(img.Bounds())
	if box.Empty() {
		return
	}
	m := &mask{rect: box, c: make([]float32, box.Dx()*box.Dy())}
	half := w / 2
	for _, line := range lines {
		if len(line) == 1 {
			strokeSegment(m, line[0], line[0], half)
		}
		for i := 1; i < len(line); i++ {
			strokeSegment(m, line[i-1], line[i], half)
		}
	}
	if tri != nil {
		fillTriangle(m, *tri)
	}
	blend(img, m, s.Color)
}

// strokeSegment writes coverage clamp(half + 0.5 - d, 0, 1) for every pixel
// whose centre is at distance d from the segment.
func strokeSegment(m *mask, a, b annotate.FPoint, half float64) {
	r := half + 1
	x0 := max(m.rect.Min.X, int(math.Floor(math.Min(a.X, b.X)-r)))
	y0 := max(m.rect.Min.Y, int(math.Floor(math.Min(a.Y, b.Y)-r)))
	x1 := min(m.rect.Max.X, int(math.Ceil(math.Max(a.X, b.X)+r))+1)
	y1 := min(m.rect.Max.Y, int(math.Ceil(math.Max(a.Y, b.Y)+r))+1)
	dx, dy := b.X-a.X, b.Y-a.Y
	length2 := dx*dx + dy*dy
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			px := float64(x) + 0.5
			t := 0.0
			if length2 > 0 {
				t = ((px-a.X)*dx + (py-a.Y)*dy) / length2
				t = math.Max(0, math.Min(1, t))
			}
			d := math.Hypot(px-(a.X+t*dx), py-(a.Y+t*dy))
			c := half + 0.5 - d
			if c <= 0 {
				continue
			}
			if c > 1 {
				c = 1
			}
			m.set(x, y, float32(c))
		}
	}
}

// fillTriangle writes 4×4-supersampled coverage of a solid triangle.
func fillTriangle(m *mask, t [3]annotate.FPoint) {
	x0 := max(m.rect.Min.X, int(math.Floor(math.Min(t[0].X, math.Min(t[1].X, t[2].X)))))
	y0 := max(m.rect.Min.Y, int(math.Floor(math.Min(t[0].Y, math.Min(t[1].Y, t[2].Y)))))
	x1 := min(m.rect.Max.X, int(math.Ceil(math.Max(t[0].X, math.Max(t[1].X, t[2].X))))+1)
	y1 := min(m.rect.Max.Y, int(math.Ceil(math.Max(t[0].Y, math.Max(t[1].Y, t[2].Y))))+1)
	edge := func(a, b annotate.FPoint, x, y float64) float64 {
		return (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X)
	}
	area := edge(t[0], t[1], t[2].X, t[2].Y)
	if area == 0 {
		return
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			inside := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px, py := float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4
					e0, e1, e2 := edge(t[0], t[1], px, py), edge(t[1], t[2], px, py), edge(t[2], t[0], px, py)
					if (area > 0 && e0 >= 0 && e1 >= 0 && e2 >= 0) || (area < 0 && e0 <= 0 && e1 <= 0 && e2 <= 0) {
						inside++
					}
				}
			}
			if inside > 0 {
				m.set(x, y, float32(inside)/16)
			}
		}
	}
}

// blend composites colour × coverage source-over onto the straight-alpha image.
func blend(img *image.RGBA, m *mask, c annotate.Color) {
	ca := float32(c.A) / 255
	for y := m.rect.Min.Y; y < m.rect.Max.Y; y++ {
		row := m.c[(y-m.rect.Min.Y)*m.rect.Dx():]
		for x := m.rect.Min.X; x < m.rect.Max.X; x++ {
			cov := row[x-m.rect.Min.X] * ca
			if cov <= 0 {
				continue
			}
			p := img.Pix[img.PixOffset(x, y):]
			da := float32(p[3]) / 255
			oa := cov + da*(1-cov)
			if oa <= 0 {
				continue
			}
			mix := func(src uint8, dst uint8) uint8 {
				v := (float32(src)*cov + float32(dst)*da*(1-cov)) / oa
				return uint8(v + 0.5)
			}
			p[0], p[1], p[2] = mix(c.R, p[0]), mix(c.G, p[1]), mix(c.B, p[2])
			p[3] = uint8(oa*255 + 0.5)
		}
	}
}
