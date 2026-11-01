package overlay

import (
	"fmt"
	"image"
	"math"
	"time"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/capture"
	sui "snaplite/internal/ui"
)

// Phase is where one overlay is in the selection flow.
type Phase uint8

const (
	PhaseIdle      Phase = iota // nothing selected yet
	PhaseSelecting              // dragging out a new selection
	PhaseEditing                // a selection exists
)

// Drag is what the pointer is dragging.
type Drag uint8

const (
	DragNone Drag = iota
	DragNew
	DragMove
	DragHandle
	DragDraw
)

// State is one overlay window: one display of the session.
type State struct {
	Phase     Phase
	Frame     *capture.Frame
	Sel       vui.Rect // window logical pixels, normalised
	Drag      Drag
	Handle    int // the handle being dragged, 0..7 clockwise from top-left
	DragFrom  vui.Point
	SelAtDrag vui.Rect
	Hover     vui.Point
	HasHover  bool
	Snap      vui.Rect // window-snapping candidate under the pointer (P1)
	HasSnap   bool
	Doc       annotate.Document

	// windows are the snapping candidates in this window's coordinates.
	windows []vui.Rect
	// label is the size label's text, "1280 × 720", kept in step with Sel so
	// Paint does not format strings.
	label string
	// pixel is the magnifier's colour text, "#RRGGBB", kept in step with Hover.
	pixel    string
	pixelRGB [3]uint8
	moved    bool
	// hintStart is the canvas time of the first paint, for the hint fade.
	hintStart   time.Duration
	hintStarted bool
}

func newState(frame *capture.Frame) *State {
	st := &State{Frame: frame, Doc: annotate.NewDocument()}
	st.Doc.Source, st.Doc.Scale = frame.Image, frame.DisplayScale()
	return st
}

// size is the overlay window's logical size.
func (s *State) size() vui.Size { return s.Frame.Size() }

// fit adopts the size the window really paints at, when it is not the one
// the frame expects: then the toolkit's scale for the window is not the
// display's, and mapping by the expected size would cover only part of the
// picture (a quarter of a 200% display taken for 100%). Everything held in
// window coordinates is scaled along. It reports whether the size changed.
func (s *State) fit(size vui.Size) bool {
	old := s.size()
	if size.Width <= 0 || size.Height <= 0 || old.Width <= 0 || old.Height <= 0 ||
		abs32(size.Width-old.Width) < 0.5 && abs32(size.Height-old.Height) < 0.5 {
		return false
	}
	kx, ky := size.Width/old.Width, size.Height/old.Height
	scale := func(r vui.Rect) vui.Rect {
		return vui.Rect{X: r.X * kx, Y: r.Y * ky, Width: r.Width * kx, Height: r.Height * ky}
	}
	s.Frame.View = size
	for i, w := range s.windows {
		s.windows[i] = scale(w)
	}
	s.Snap = scale(s.Snap)
	s.Hover = vui.Point{X: s.Hover.X * kx, Y: s.Hover.Y * ky}
	s.DragFrom = vui.Point{X: s.DragFrom.X * kx, Y: s.DragFrom.Y * ky}
	s.SelAtDrag = scale(s.SelAtDrag)
	if !empty(s.Sel) {
		s.setSel(scale(s.Sel))
	}
	return true
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// setSel replaces the selection, normalising, clamping it to the window and
// updating the size label.
func (s *State) setSel(r vui.Rect) {
	r = normalize(r)
	size := s.size()
	r = clampRect(r, size)
	s.Sel = r
	img := s.Frame.RectToImage(r)
	s.label = fmt.Sprintf("%d × %d", img.Dx(), img.Dy())
}

func (s *State) clearSel() {
	s.Sel = vui.Rect{}
	s.label = ""
}

// setHover records the pointer and the colour under it for the magnifier.
func (s *State) setHover(p vui.Point) {
	s.Hover, s.HasHover = p, true
	ip := s.Frame.ToImage(p)
	if !ip.In(s.Frame.Image.Bounds()) {
		return
	}
	c := s.Frame.Image.RGBAAt(ip.X, ip.Y)
	rgb := [3]uint8{c.R, c.G, c.B}
	if rgb != s.pixelRGB || s.pixel == "" {
		s.pixelRGB = rgb
		s.pixel = fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	}
}

// updateSnap finds the front-most window under the pointer.
func (s *State) updateSnap() {
	s.HasSnap = false
	for _, w := range s.windows {
		if contains(w, s.Hover) {
			s.Snap, s.HasSnap = w, true
			return
		}
	}
}

func (s *State) view() annotate.View {
	return annotate.View{Zoom: float32(1 / s.Frame.Scale())}
}

func (s *State) toImage(p vui.Point) image.Point { return s.Frame.ToImage(p) }

func normalize(r vui.Rect) vui.Rect {
	if r.Width < 0 {
		r.X += r.Width
		r.Width = -r.Width
	}
	if r.Height < 0 {
		r.Y += r.Height
		r.Height = -r.Height
	}
	return r
}

// clampRect keeps r inside a window of the given size (intersection).
func clampRect(r vui.Rect, size vui.Size) vui.Rect {
	x0, y0 := max(r.X, 0), max(r.Y, 0)
	x1, y1 := min(r.X+r.Width, size.Width), min(r.Y+r.Height, size.Height)
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return vui.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// moveInside offsets r by (dx, dy) without leaving a window of size.
func moveInside(r vui.Rect, dx, dy float32, size vui.Size) vui.Rect {
	r.X = min(max(r.X+dx, 0), max(size.Width-r.Width, 0))
	r.Y = min(max(r.Y+dy, 0), max(size.Height-r.Height, 0))
	return r
}

func contains(r vui.Rect, p vui.Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.Width && p.Y < r.Y+r.Height
}

func empty(r vui.Rect) bool { return r.Width <= 0 || r.Height <= 0 }

// handlePoints are the eight handle centres clockwise from the top-left.
func handlePoints(r vui.Rect) [8]vui.Point {
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	mx, my := (x0+x1)/2, (y0+y1)/2
	return [8]vui.Point{{X: x0, Y: y0}, {X: mx, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: my}, {X: x1, Y: y1}, {X: mx, Y: y1}, {X: x0, Y: y1}, {X: x0, Y: my}}
}

// handleReach is how far from a handle's centre a press still grabs it.
const handleReach = 6

func handleRect(p vui.Point) vui.Rect {
	return vui.Rect{X: p.X - handleReach, Y: p.Y - handleReach, Width: 2 * handleReach, Height: 2 * handleReach}
}

// handleAt returns the handle under p, or -1.
func handleAt(r vui.Rect, p vui.Point) int {
	if empty(r) {
		return -1
	}
	for i, h := range handlePoints(r) {
		if math.Abs(float64(p.X-h.X)) <= handleReach && math.Abs(float64(p.Y-h.Y)) <= handleReach {
			return i
		}
	}
	return -1
}

// resize moves the edges a handle controls by the pointer's travel.
func resize(r vui.Rect, handle int, dx, dy float32) vui.Rect {
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	switch handle {
	case 0:
		x0, y0 = x0+dx, y0+dy
	case 1:
		y0 += dy
	case 2:
		x1, y0 = x1+dx, y0+dy
	case 3:
		x1 += dx
	case 4:
		x1, y1 = x1+dx, y1+dy
	case 5:
		y1 += dy
	case 6:
		x0, y1 = x0+dx, y1+dy
	case 7:
		x0 += dx
	}
	return normalize(vui.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0})
}

func handleCursor(i int) vui.PointerCursor {
	switch i {
	case 0, 4:
		return vui.PointerCursorResizeNWSE
	case 2, 6:
		return vui.PointerCursorResizeNESW
	case 1, 5:
		return vui.PointerCursorResizeVertical
	}
	return vui.PointerCursorResizeHorizontal
}

// Placement says where the toolbar went relative to the selection.
type Placement uint8

const (
	PlaceBelow Placement = iota
	PlaceAbove
	PlaceInside
)

// PlaceToolbar positions a bar of size bar for the selection sel on a screen
// of size screen (docs/02 §3.1): right-aligned under the selection, else
// above it, else inside its bottom edge; horizontally always on screen.
// extra is room needed beyond the bar on its far side (the property bar).
func PlaceToolbar(sel vui.Rect, bar vui.Size, screen vui.Size, extra float32) (vui.Rect, Placement) {
	gap := float32(sui.FloatGap)
	x := sel.X + sel.Width - bar.Width
	x = min(max(x, 0), max(screen.Width-bar.Width, 0))
	r := vui.Rect{X: x, Width: bar.Width, Height: bar.Height}
	if below := sel.Y + sel.Height + gap; below+bar.Height+extra <= screen.Height {
		r.Y = below
		return r, PlaceBelow
	}
	if above := sel.Y - gap - bar.Height; above-extra >= 0 {
		r.Y = above
		return r, PlaceAbove
	}
	r.Y = max(sel.Y+sel.Height-gap-bar.Height-extra, 0)
	return r, PlaceInside
}

// labelRect places the size label above the selection's top-left corner, or
// inside it when there is no room.
func labelRect(sel vui.Rect, w, h float32) vui.Rect {
	r := vui.Rect{X: sel.X, Y: sel.Y - 6 - h, Width: w, Height: h}
	if r.Y < 0 {
		r.Y = sel.Y + 6
		r.X = sel.X + 6
	}
	return r
}
