package overlay

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/capture"
	"snaplite/internal/i18n"
	sui "snaplite/internal/ui"
)

var (
	feature     = sui.Tree.Feature("overlay")
	surfaceID   = sui.Tree.ID(feature, "surface")
	selectionID = sui.Tree.ID(feature, "selection")
	handleScope = sui.Tree.Sub(feature, sui.Tree.Name("handles"))
)

// ResultKind is how a capture session ended.
type ResultKind uint8

const (
	Done ResultKind = iota
	Save
	// Pin puts the picture on the desktop in its own window, where it was.
	Pin
	Cancel
)

func (k ResultKind) String() string {
	return [...]string{"Done", "Save", "Pin", "Cancel"}[k]
}

// Result is what the overlay hands to the shell.
type Result struct {
	Kind ResultKind
	// Frame is the display the selection is on; nil for Cancel.
	Frame *capture.Frame
	// Sel is the selection in image pixels of Frame.Image.
	Sel image.Rectangle
	// SelWindow is the selection in the overlay window's logical pixels.
	SelWindow vui.Rect
	Shapes    []annotate.Shape
}

// Controller owns the overlays of one session: a State per display and the
// single active selection.
type Controller struct {
	Session  *capture.Session
	States   []*State
	active   int // index of the state with the selection, -1 before the first press
	tokens   sui.Tokens
	onResult func(Result)
	finished bool
	wakes    []*app.Invalidator
}

// HintRegion is the capsule text (docs/02 §3.1).
func HintRegion() string {
	return i18n.T("拖动选择区域 · 右键或 Esc 退出", "Drag to select an area · Right-click or Esc to exit")
}

const (
	hintShown = 2 * time.Second
	hintFade  = 200 * time.Millisecond
)

// NewController makes the overlays of a session. onResult runs once, on the
// event-loop thread, when the session ends.
func NewController(session *capture.Session, tokens sui.Tokens, onResult func(Result)) *Controller {
	c := &Controller{Session: session, active: -1, tokens: tokens, onResult: onResult}
	for i := range session.Frames {
		f := &session.Frames[i]
		st := newState(f)
		b := f.Display.Bounds
		for _, w := range session.Windows {
			r := clampRect(vui.Rect{X: float32(w.X - b.X), Y: float32(w.Y - b.Y), Width: float32(w.W), Height: float32(w.H)}, st.size())
			if !empty(r) {
				st.windows = append(st.windows, r)
			}
		}
		c.States = append(c.States, st)
		c.wakes = append(c.wakes, &app.Invalidator{})
	}
	return c
}

// WindowTitle is the unique title the platform layer finds an overlay by.
func WindowTitle(f *capture.Frame) string { return fmt.Sprintf("snaplite-overlay-%d", f.Display.ID) }

// Finished reports whether the session has produced its result.
func (c *Controller) Finished() bool { return c.finished }

// Config is the window configuration of overlay i.
func (c *Controller) Config(i int) app.Config {
	st := c.States[i]
	b := st.Frame.Display.Bounds
	return app.Config{
		Title:       WindowTitle(st.Frame),
		WindowMode:  app.WindowFrameless,
		Width:       int(b.W + 0.5),
		Height:      int(b.H + 0.5),
		Origin:      &vui.Point{X: float32(b.X), Y: float32(b.Y)},
		ClearColor:  app.Color{Alpha: 1},
		Tree:        &sui.Tree,
		Paint:       func(canvas *vui.Canvas, size vui.Size) { c.paint(i, canvas, size) },
		OnPointer:   func(e vui.PointerEvent) vui.EventResult { return c.pointer(i, e) },
		OnKey:       func(e vui.KeyEvent) vui.EventResult { return c.key(i, e) },
		OnShortcut:  func(e vui.KeyEvent) vui.EventResult { return c.shortcut(i, e) },
		Invalidator: c.wakes[i],
	}
}

// keyState is the state keys act on: the active one, else the window's own.
func (c *Controller) keyState(i int) *State {
	if c.active >= 0 {
		return c.States[c.active]
	}
	return c.States[i]
}

func (c *Controller) activate(i int) {
	if c.active == i {
		return
	}
	c.active = i
	for j, w := range c.wakes {
		if j != i {
			w.Invalidate()
		}
	}
}

func (c *Controller) release(st *State) {
	st.Phase, st.Drag = PhaseIdle, DragNone
	st.clearSel()
	c.active = -1
}

// finish ends the session with a result.
func (c *Controller) finish(kind ResultKind) {
	if c.finished {
		return
	}
	res := Result{Kind: kind}
	if kind != Cancel {
		if c.active < 0 {
			return
		}
		st := c.States[c.active]
		if st.Phase != PhaseEditing || empty(st.Sel) {
			return
		}
		st.Doc.CancelDraft()
		res.Frame = st.Frame
		res.SelWindow = st.Sel
		res.Sel = st.Frame.RectToImage(st.Sel)
		res.Shapes = st.Doc.Snapshot()
		if res.Sel.Empty() {
			return
		}
	}
	c.finished = true
	if c.onResult != nil {
		c.onResult(res)
	}
}

// escape is Escape and the secondary button (docs/05 §6.2).
func (c *Controller) escape(st *State) {
	switch st.Phase {
	case PhaseSelecting:
		c.release(st)
	case PhaseEditing:
		if st.Drag == DragDraw || st.Doc.Draft != nil {
			st.Doc.CancelDraft()
			st.Drag = DragNone
			return
		}
		c.finish(Cancel)
	default:
		c.finish(Cancel)
	}
}

var handled = vui.EventResult{Handled: true, Repaint: true}

func (c *Controller) pointer(i int, e vui.PointerEvent) vui.EventResult {
	st := c.States[i]
	if c.finished || (c.active >= 0 && c.active != i) {
		return vui.EventResult{}
	}
	p := vui.Point{X: e.X, Y: e.Y}
	shift := e.Modifiers&vui.ModifierShift != 0
	switch e.Kind {
	case vui.PointerExit:
		st.HasHover = false
		return vui.EventResult{Repaint: st.Phase != PhaseEditing}
	case vui.PointerMove:
		st.setHover(p)
		if st.Drag != DragNone {
			c.drag(st, p, shift)
			return handled
		}
		if st.Phase == PhaseIdle {
			st.updateSnap()
			return vui.EventResult{Repaint: true}
		}
		return vui.EventResult{}
	case vui.PointerDown:
		if sui.IsToolbar(e.Target) {
			return vui.EventResult{}
		}
		if e.Button == vui.PointerButtonSecondary {
			c.escape(st)
			return handled
		}
		if e.Button != vui.PointerButtonPrimary {
			return vui.EventResult{}
		}
		st.setHover(p)
		c.press(i, st, p, e.ClickCount)
		return handled
	case vui.PointerUp:
		if e.Button != vui.PointerButtonPrimary || st.Drag == DragNone {
			return vui.EventResult{Handled: st.Drag != DragNone}
		}
		c.drag(st, p, shift)
		c.releaseDrag(st)
		return handled
	}
	return vui.EventResult{}
}

func (c *Controller) press(i int, st *State, p vui.Point, clicks int) {
	st.DragFrom, st.SelAtDrag, st.moved = p, st.Sel, false
	switch st.Phase {
	case PhaseIdle:
		c.activate(i)
		st.Phase, st.Drag = PhaseSelecting, DragNew
		st.setSel(vui.Rect{X: p.X, Y: p.Y})
	case PhaseEditing:
		if clicks == 2 && contains(st.Sel, p) {
			c.finish(Done)
			return
		}
		if h := handleAt(st.Sel, p); h >= 0 {
			st.Drag, st.Handle = DragHandle, h
			return
		}
		if contains(st.Sel, p) {
			if st.Doc.Tool != annotate.ToolNone {
				st.Doc.Begin(st.toImage(p))
				st.Drag = DragDraw
				return
			}
			st.Drag = DragMove
			return
		}
		st.Phase, st.Drag = PhaseSelecting, DragNew
		st.setSel(vui.Rect{X: p.X, Y: p.Y})
	}
}

func (c *Controller) drag(st *State, p vui.Point, shift bool) {
	dx, dy := p.X-st.DragFrom.X, p.Y-st.DragFrom.Y
	if dx*dx+dy*dy > 4 {
		st.moved = true
	}
	switch st.Drag {
	case DragNew:
		st.setSel(vui.Rect{X: st.DragFrom.X, Y: st.DragFrom.Y, Width: dx, Height: dy})
	case DragMove:
		st.setSel(moveInside(st.SelAtDrag, dx, dy, st.size()))
	case DragHandle:
		st.setSel(resize(st.SelAtDrag, st.Handle, dx, dy))
	case DragDraw:
		st.Doc.Extend(st.toImage(p), shift)
	}
}

// minSelection is the smallest selection kept, in logical pixels.
const minSelection = 4

func (c *Controller) releaseDrag(st *State) {
	drag := st.Drag
	st.Drag = DragNone
	switch drag {
	case DragNew:
		if st.Sel.Width < minSelection || st.Sel.Height < minSelection {
			if !st.moved && st.HasSnap {
				st.setSel(st.Snap)
			} else {
				c.release(st)
				return
			}
		}
		st.Phase = PhaseEditing
		st.HasSnap = false
	case DragDraw:
		st.Doc.Commit()
	}
}

func (c *Controller) key(i int, e vui.KeyEvent) vui.EventResult {
	if c.finished {
		return vui.EventResult{}
	}
	st := c.keyState(i)
	switch e.Key {
	case vui.KeyEscape:
		c.escape(st)
		return handled
	case vui.KeyEnter:
		if st.Phase == PhaseEditing {
			c.finish(Done)
		}
		return handled
	case vui.KeyLeft, vui.KeyRight, vui.KeyUp, vui.KeyDown:
		if st.Phase != PhaseEditing || st.Drag != DragNone {
			return vui.EventResult{}
		}
		step := float32(1)
		if e.Modifiers&vui.ModifierShift != 0 {
			step = 10
		}
		dx, dy := float32(0), float32(0)
		switch e.Key {
		case vui.KeyLeft:
			dx = -step
		case vui.KeyRight:
			dx = step
		case vui.KeyUp:
			dy = -step
		case vui.KeyDown:
			dy = step
		}
		st.setSel(moveInside(st.Sel, dx, dy, st.size()))
		return handled
	}
	return vui.EventResult{}
}

// primary is the platform's command modifier: Command on macOS, Control on Windows.
const primaryMods = vui.ModifierMeta | vui.ModifierControl

func (c *Controller) shortcut(i int, e vui.KeyEvent) vui.EventResult {
	if c.finished || e.Modifiers&primaryMods == 0 {
		return vui.EventResult{}
	}
	st := c.keyState(i)
	shift := e.Modifiers&vui.ModifierShift != 0
	switch strings.ToLower(e.Text) {
	case "z":
		if shift {
			st.Doc.Redo()
		} else {
			st.Doc.Undo()
		}
	case "y":
		st.Doc.Redo()
	case "s":
		c.finish(Save)
	case "c":
		if shift || st.Phase != PhaseEditing {
			// Copy the colour under the pointer (the magnifier's value).
			if st.pixel == "" {
				return vui.EventResult{Handled: true}
			}
			return vui.EventResult{Handled: true, ClipboardText: st.pixel}
		}
		c.finish(Done)
	case "1", "2", "3", "4", "5":
		if st.Phase == PhaseEditing {
			t := annotate.Tool(e.Text[0]-'1') + annotate.ToolRect
			c.toggleTool(st, t)
		}
	default:
		return vui.EventResult{}
	}
	return handled
}

func (c *Controller) toggleTool(st *State, t annotate.Tool) {
	if st.Doc.Tool == t {
		st.Doc.Tool = annotate.ToolNone
	} else {
		st.Doc.Tool = t
	}
}

// toolbarSpec is the toolbar's buttons and their state.
func (c *Controller) toolbarSpec(st *State) sui.ToolbarSpec {
	return sui.ToolbarSpec{
		Tools: true, Pin: true, Save: true, Cancel: true, Done: true,
		Tool: st.Doc.Tool, CanUndo: st.Doc.CanUndo(), CanRedo: st.Doc.CanRedo(),
	}
}

// act applies a toolbar click; it runs after the paint that saw the click.
func (c *Controller) act(st *State, a sui.Action) {
	switch a {
	case sui.ActRect, sui.ActEllipse, sui.ActArrow, sui.ActPen, sui.ActMosaic:
		c.toggleTool(st, sui.ActionTool(a))
	case sui.ActUndo:
		st.Doc.Undo()
	case sui.ActRedo:
		st.Doc.Redo()
	case sui.ActPin:
		c.finish(Pin)
	case sui.ActSave:
		c.finish(Save)
	case sui.ActCancel:
		c.finish(Cancel)
	case sui.ActDone:
		c.finish(Done)
	}
}
