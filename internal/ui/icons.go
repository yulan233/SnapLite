package ui

import (
	"image"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/raster"
)

// Icon names a line icon drawn in a 20×20 view box with a 1.5px stroke
// (docs/02 §1.3). Icons are built in the canvas's shared path builder at
// paint time, which allocates nothing once warm.
type Icon uint8

const (
	IconRect Icon = iota
	IconEllipse
	IconArrow
	IconPen
	IconUndo
	IconRedo
	IconLongShot
	IconSave
	IconCancel
	IconDone
	IconCopy
	IconSearch
	IconClipboard
)

// DrawIcon paints icon centred in bounds.
func DrawIcon(canvas *vui.Canvas, icon Icon, bounds vui.Rect, color vui.Color) {
	box := bounds.Centered(IconSize, IconSize)
	b := canvas.PathBuilder()
	switch icon {
	case IconRect:
		b.MoveTo(3.5, 4.5)
		b.LineTo(16.5, 4.5)
		b.LineTo(16.5, 15.5)
		b.LineTo(3.5, 15.5)
		b.Close()
	case IconEllipse:
		const k = 0.5523
		cx, cy, rx, ry := float32(10), float32(10), float32(7), float32(6)
		b.MoveTo(cx+rx, cy)
		b.CubicTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
		b.CubicTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
		b.CubicTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
		b.CubicTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
		b.Close()
	case IconArrow:
		b.MoveTo(4, 16)
		b.LineTo(15.5, 4.5)
		b.MoveTo(8.5, 4.5)
		b.LineTo(15.5, 4.5)
		b.LineTo(15.5, 11.5)
	case IconPen:
		b.MoveTo(4, 16)
		b.LineTo(4.8, 12.6)
		b.LineTo(13, 4.4)
		b.LineTo(15.6, 7)
		b.LineTo(7.4, 15.2)
		b.Close()
		b.MoveTo(11.4, 6)
		b.LineTo(14, 8.6)
	case IconUndo:
		b.MoveTo(7, 5)
		b.LineTo(4, 8)
		b.LineTo(7, 11)
		b.MoveTo(4, 8)
		b.LineTo(12, 8)
		b.CubicTo(14.5, 8, 16, 9.8, 16, 12)
		b.CubicTo(16, 14.2, 14.5, 16, 12, 16)
		b.LineTo(8, 16)
	case IconRedo:
		b.MoveTo(13, 5)
		b.LineTo(16, 8)
		b.LineTo(13, 11)
		b.MoveTo(16, 8)
		b.LineTo(8, 8)
		b.CubicTo(5.5, 8, 4, 9.8, 4, 12)
		b.CubicTo(4, 14.2, 5.5, 16, 8, 16)
		b.LineTo(12, 16)
	case IconLongShot:
		b.MoveTo(5, 3.5)
		b.LineTo(15, 3.5)
		b.LineTo(15, 9)
		b.MoveTo(5, 9)
		b.LineTo(5, 3.5)
		b.MoveTo(10, 8)
		b.LineTo(10, 16.5)
		b.MoveTo(6.5, 13)
		b.LineTo(10, 16.5)
		b.LineTo(13.5, 13)
	case IconSave:
		b.MoveTo(10, 3.5)
		b.LineTo(10, 12.5)
		b.MoveTo(6.5, 9)
		b.LineTo(10, 12.5)
		b.LineTo(13.5, 9)
		b.MoveTo(4, 13.5)
		b.LineTo(4, 16.5)
		b.LineTo(16, 16.5)
		b.LineTo(16, 13.5)
	case IconCancel:
		b.MoveTo(5, 5)
		b.LineTo(15, 15)
		b.MoveTo(15, 5)
		b.LineTo(5, 15)
	case IconDone:
		b.MoveTo(4.5, 10.5)
		b.LineTo(8.5, 14.5)
		b.LineTo(15.5, 5.5)
	case IconCopy:
		b.MoveTo(7, 7)
		b.LineTo(16, 7)
		b.LineTo(16, 16)
		b.LineTo(7, 16)
		b.Close()
		b.MoveTo(4, 13)
		b.LineTo(4, 4)
		b.LineTo(13, 4)
	case IconSearch:
		const k = 0.5523
		cx, cy, r := float32(8.5), float32(8.5), float32(5)
		b.MoveTo(cx+r, cy)
		b.CubicTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
		b.CubicTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
		b.CubicTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
		b.CubicTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
		b.Close()
		b.MoveTo(12.2, 12.2)
		b.LineTo(16.5, 16.5)
	case IconClipboard:
		b.MoveTo(7, 4.5)
		b.LineTo(5, 4.5)
		b.LineTo(5, 17)
		b.LineTo(15, 17)
		b.LineTo(15, 4.5)
		b.LineTo(13, 4.5)
		b.MoveTo(7.5, 3)
		b.LineTo(12.5, 3)
		b.LineTo(12.5, 6)
		b.LineTo(7.5, 6)
		b.Close()
	}
	canvas.DrawPath(b.Path(IconSize, IconSize), box, vui.PathStyle{Stroke: color, StrokeWidth: 1.5, Cap: vui.CapRound, Join: vui.JoinRound})
}

// TrayIcon renders the status-area icon: a viewfinder's four corners and a
// dot, size×size straight RGBA. A template icon only uses its alpha.
func TrayIcon(size int, c annotate.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := func(v float64) int { return int(v * float64(size) / 32) }
	w := float32(2.6 * float64(size) / 32)
	line := func(a, b image.Point) annotate.Shape {
		return annotate.Shape{Tool: annotate.ToolPen, Color: c, Width: w, Points: []image.Point{a, b}}
	}
	lo, hi, arm := s(4), s(28), s(8)
	shapes := []annotate.Shape{
		line(image.Pt(lo, lo+arm), image.Pt(lo, lo)), line(image.Pt(lo, lo), image.Pt(lo+arm, lo)),
		line(image.Pt(hi-arm, lo), image.Pt(hi, lo)), line(image.Pt(hi, lo), image.Pt(hi, lo+arm)),
		line(image.Pt(hi, hi-arm), image.Pt(hi, hi)), line(image.Pt(hi, hi), image.Pt(hi-arm, hi)),
		line(image.Pt(lo+arm, hi), image.Pt(lo, hi)), line(image.Pt(lo, hi), image.Pt(lo, hi-arm)),
		{Tool: annotate.ToolPen, Color: c, Width: float32(6 * float64(size) / 32), Points: []image.Point{{size / 2, size / 2}}},
	}
	return raster.Compose(img, shapes).Pix
}
