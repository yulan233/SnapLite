package annotate

import (
	"image"
	"math"
)

// Tool is the annotation tool in use.
type Tool uint8

const (
	ToolNone Tool = iota
	ToolRect
	ToolEllipse
	ToolArrow
	ToolPen
)

// Color is a straight-alpha 8-bit colour.
type Color struct{ R, G, B, A uint8 }

// Palette is the six annotation colours, red first (the default).
var Palette = [...]Color{
	{0xF0, 0x44, 0x38, 0xFF},
	{0xF5, 0xB8, 0x00, 0xFF},
	{0x12, 0xB7, 0x6A, 0xFF},
	{0x2E, 0x90, 0xFA, 0xFF},
	{0x10, 0x18, 0x28, 0xFF},
	{0xFF, 0xFF, 0xFF, 0xFF},
}

// Widths are the three stroke widths in image pixels; 4 is the default.
var Widths = [...]float32{2, 4, 8}

// Shape is one committed annotation, in image pixels.
type Shape struct {
	Tool  Tool
	Color Color
	Width float32
	// Points are [start, end] for rectangles, ellipses and arrows, and the
	// smoothed samples of a pen stroke.
	Points []image.Point
}

// Document is the annotations of one image and their undo history.
type Document struct {
	Shapes []Shape
	redo   []Shape
	// Draft is the stroke being drawn; Commit adds it to Shapes.
	Draft *Shape
	Tool  Tool
	Color Color
	Width float32

	// paths caches each committed shape's screen geometry, parallel to
	// Shapes; redoPaths is parallel to redo.
	paths     []shapePath
	redoPaths []shapePath
	// changes counts edits since MarkClean.
	changes int
}

// NewDocument returns a document with the default colour and width.
func NewDocument() Document {
	return Document{Color: Palette[0], Width: Widths[1]}
}

// minPenStep is the distance below which a new pen sample is dropped.
const minPenStep = 2

// Begin starts a draft with the current tool at p.
func (d *Document) Begin(p image.Point) {
	if d.Tool == ToolNone {
		return
	}
	if d.Width <= 0 {
		d.Width = Widths[1]
	}
	if d.Color == (Color{}) {
		d.Color = Palette[0]
	}
	d.Draft = &Shape{Tool: d.Tool, Color: d.Color, Width: d.Width, Points: []image.Point{p, p}}
	if d.Tool == ToolPen {
		d.Draft.Points = d.Draft.Points[:1]
	}
}

// Extend moves the draft's end to p. shift squares rectangles and ellipses
// and snaps arrows to 45°.
func (d *Document) Extend(p image.Point, shift bool) {
	s := d.Draft
	if s == nil {
		return
	}
	if s.Tool == ToolPen {
		last := s.Points[len(s.Points)-1]
		if math.Hypot(float64(p.X-last.X), float64(p.Y-last.Y)) < minPenStep {
			return
		}
		s.Points = append(s.Points, p)
		return
	}
	if shift {
		p = Constrain(s.Tool, s.Points[0], p)
	}
	s.Points[1] = p
}

// tooSmall reports whether a shape is too small to keep (under 3 pixels).
func tooSmall(s *Shape) bool {
	minX, minY, maxX, maxY := s.Points[0].X, s.Points[0].Y, s.Points[0].X, s.Points[0].Y
	for _, p := range s.Points {
		minX, maxX = min(minX, p.X), max(maxX, p.X)
		minY, maxY = min(minY, p.Y), max(maxY, p.Y)
	}
	return maxX-minX < 3 && maxY-minY < 3
}

// Commit adds the draft to the document, dropping shapes under 3 pixels.
// It reports whether a shape was added.
func (d *Document) Commit() bool {
	s := d.Draft
	d.Draft = nil
	if s == nil || tooSmall(s) {
		return false
	}
	d.Shapes = append(d.Shapes, *s)
	d.paths = append(d.paths, buildPath(*s))
	d.redo, d.redoPaths = d.redo[:0], d.redoPaths[:0]
	d.changes++
	return true
}

// CancelDraft drops the stroke being drawn and reports whether there was one.
func (d *Document) CancelDraft() bool {
	had := d.Draft != nil
	d.Draft = nil
	return had
}

// Undo removes the last shape.
func (d *Document) Undo() bool {
	n := len(d.Shapes)
	if n == 0 {
		return false
	}
	d.redo = append(d.redo, d.Shapes[n-1])
	d.redoPaths = append(d.redoPaths, d.paths[n-1])
	d.Shapes, d.paths = d.Shapes[:n-1], d.paths[:n-1]
	d.changes++
	return true
}

// Redo restores the last undone shape.
func (d *Document) Redo() bool {
	n := len(d.redo)
	if n == 0 {
		return false
	}
	d.Shapes = append(d.Shapes, d.redo[n-1])
	d.paths = append(d.paths, d.redoPaths[n-1])
	d.redo, d.redoPaths = d.redo[:n-1], d.redoPaths[:n-1]
	d.changes++
	return true
}

// CanUndo and CanRedo report whether Undo or Redo would do anything.
func (d *Document) CanUndo() bool { return len(d.Shapes) > 0 }
func (d *Document) CanRedo() bool { return len(d.redo) > 0 }

// MarkClean records that the current shapes were copied or saved.
func (d *Document) MarkClean() { d.changes = 0 }

// Dirty reports whether shapes changed since MarkClean (or since creation).
func (d *Document) Dirty() bool { return d.changes > 0 }

// Snapshot returns a copy of the committed shapes for compositing elsewhere.
func (d *Document) Snapshot() []Shape {
	out := make([]Shape, len(d.Shapes))
	for i, s := range d.Shapes {
		s.Points = append([]image.Point(nil), s.Points...)
		out[i] = s
	}
	return out
}
