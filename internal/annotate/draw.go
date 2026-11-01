package annotate

import (
	"image"

	vui "github.com/vivaui/viva/ui"
)

// viewBox is the side of the view box every annotation path is built in.
// Paths hold image-pixel coordinates; DrawPath maps the view box linearly
// onto bounds, so drawing the same cached path at another zoom or scroll
// only changes bounds. Coordinates outside the box map just as well.
const viewBox = 1024

// shapePath is the cached screen geometry of one committed shape.
type shapePath struct {
	stroke vui.Path
	head   vui.Path // the arrow's solid head; empty otherwise
}

// ToColor converts an annotation colour for Viva.
func ToColor(c Color) vui.Color { return vui.RGBA(c.R, c.G, c.B, float32(c.A)/255) }

func f32(p FPoint) (float32, float32) { return float32(p.X), float32(p.Y) }

// commands appends the stroke commands of s to dst.
func commands(dst []vui.PathCommand, s Shape) []vui.PathCommand {
	switch s.Tool {
	case ToolRect:
		x0, y0, x1, y1 := Box(s.Points[0], s.Points[1])
		dst = append(dst, vui.MoveTo(float32(x0), float32(y0)), vui.LineTo(float32(x1), float32(y0)),
			vui.LineTo(float32(x1), float32(y1)), vui.LineTo(float32(x0), float32(y1)), vui.ClosePath())
	case ToolEllipse:
		x0, y0, x1, y1 := Box(s.Points[0], s.Points[1])
		cx, cy := float32((x0+x1)/2), float32((y0+y1)/2)
		rx, ry := float32((x1-x0)/2), float32((y1-y0)/2)
		const k = 0.5522847498 // cubic Bézier circle constant
		dst = append(dst,
			vui.MoveTo(cx+rx, cy),
			vui.CubicTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry),
			vui.CubicTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy),
			vui.CubicTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry),
			vui.CubicTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy),
			vui.ClosePath())
	case ToolArrow:
		g := Arrow(s.Points[0], s.Points[1], float64(s.Width))
		x0, y0 := f32(g.Start)
		x1, y1 := f32(g.ShaftEnd)
		dst = append(dst, vui.MoveTo(x0, y0), vui.LineTo(x1, y1))
	case ToolPen:
		dst = appendPen(dst, s.Points)
	}
	return dst
}

func appendPen(dst []vui.PathCommand, points []image.Point) []vui.PathCommand {
	if len(points) == 0 {
		return dst
	}
	p0 := fp(points[0])
	x, y := f32(p0)
	dst = append(dst, vui.MoveTo(x, y))
	if len(points) == 1 {
		return append(dst, vui.LineTo(x+0.01, y))
	}
	for i := 1; i < len(points)-1; i++ {
		c := fp(points[i])
		m := mid(c, fp(points[i+1]))
		cx, cy := f32(c)
		mx, my := f32(m)
		dst = append(dst, vui.QuadTo(cx, cy, mx, my))
	}
	lx, ly := f32(fp(points[len(points)-1]))
	return append(dst, vui.LineTo(lx, ly))
}

func headCommands(dst []vui.PathCommand, s Shape) []vui.PathCommand {
	if s.Tool != ToolArrow {
		return dst
	}
	g := Arrow(s.Points[0], s.Points[1], float64(s.Width))
	ax, ay := f32(g.Head[0])
	bx, by := f32(g.Head[1])
	cx, cy := f32(g.Head[2])
	return append(dst, vui.MoveTo(ax, ay), vui.LineTo(bx, by), vui.LineTo(cx, cy), vui.ClosePath())
}

// buildPath makes the immutable cached geometry of a committed shape.
func buildPath(s Shape) shapePath {
	sp := shapePath{stroke: vui.NewPath(viewBox, viewBox, commands(nil, s)...)}
	if s.Tool == ToolArrow {
		sp.head = vui.NewPath(viewBox, viewBox, headCommands(nil, s)...)
	}
	return sp
}

// View maps image pixels to window coordinates: window = Origin + image*Zoom.
type View struct {
	Origin vui.Point
	Zoom   float32 // window logical pixels per image pixel (1/scale on an overlay)
}

// ToWindow maps an image point to the window.
func (v View) ToWindow(p image.Point) vui.Point {
	return vui.Point{X: v.Origin.X + float32(p.X)*v.Zoom, Y: v.Origin.Y + float32(p.Y)*v.Zoom}
}

// ToImage maps a window point to the nearest image pixel.
func (v View) ToImage(p vui.Point) image.Point {
	if v.Zoom == 0 {
		return image.Point{}
	}
	return image.Point{X: roundInt((p.X - v.Origin.X) / v.Zoom), Y: roundInt((p.Y - v.Origin.Y) / v.Zoom)}
}

func roundInt(v float32) int {
	if v < 0 {
		return int(v - 0.5)
	}
	return int(v + 0.5)
}

func (v View) bounds() vui.Rect {
	return vui.Rect{X: v.Origin.X, Y: v.Origin.Y, Width: viewBox * v.Zoom, Height: viewBox * v.Zoom}
}

func style(s Shape, zoom float32) vui.PathStyle {
	return vui.PathStyle{Stroke: ToColor(s.Color), StrokeWidth: s.Width * zoom, Cap: vui.CapRound, Join: vui.JoinRound}
}

// Draw paints the committed shapes and the draft, clipped to clip. Committed
// shapes use their cached paths; only the draft is rebuilt each paint, in
// the canvas's shared builder.
func Draw(canvas *vui.Canvas, d *Document, clip vui.Rect, view View) {
	if len(d.Shapes) == 0 && d.Draft == nil {
		return
	}
	bounds := view.bounds()
	canvas.Clip(clip)
	for i := range d.Shapes {
		s := d.Shapes[i]
		sp := d.paths[i]
		canvas.DrawPath(sp.stroke, bounds, style(s, view.Zoom))
		if s.Tool == ToolArrow {
			canvas.DrawPath(sp.head, bounds, vui.PathStyle{Fill: ToColor(s.Color)})
		}
	}
	if s := d.Draft; s != nil {
		drawDraft(canvas, *s, bounds, view.Zoom)
	}
	canvas.Unclip()
}

func drawDraft(canvas *vui.Canvas, s Shape, bounds vui.Rect, zoom float32) {
	builder := canvas.PathBuilder()
	draftScratch = commands(draftScratch[:0], s)
	for _, c := range draftScratch {
		addCommand(builder, c)
	}
	canvas.DrawPath(builder.Path(viewBox, viewBox), bounds, style(s, zoom))
	if s.Tool == ToolArrow {
		builder = canvas.PathBuilder()
		draftScratch = headCommands(draftScratch[:0], s)
		for _, c := range draftScratch {
			addCommand(builder, c)
		}
		canvas.DrawPath(builder.Path(viewBox, viewBox), bounds, vui.PathStyle{Fill: ToColor(s.Color)})
	}
}

// draftScratch is reused for the draft's commands; painting runs on the one
// event-loop thread.
var draftScratch = make([]vui.PathCommand, 0, 256)

func addCommand(b *vui.PathBuilder, c vui.PathCommand) {
	switch c.Kind {
	case vui.PathCommandMove:
		b.MoveTo(c.Point.X, c.Point.Y)
	case vui.PathCommandLine:
		b.LineTo(c.Point.X, c.Point.Y)
	case vui.PathCommandQuad:
		b.QuadTo(c.Control1.X, c.Control1.Y, c.Point.X, c.Point.Y)
	case vui.PathCommandCubic:
		b.CubicTo(c.Control1.X, c.Control1.Y, c.Control2.X, c.Control2.Y, c.Point.X, c.Point.Y)
	case vui.PathCommandClose:
		b.Close()
	}
}
