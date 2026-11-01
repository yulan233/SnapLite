package overlay

import (
	"image"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/capture"
	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// rig is one overlay of a 800×600-point display captured at 2×.
type rig struct {
	t       *testing.T
	c       *Controller
	d       *app.Driver
	canvas  *vui.Canvas
	results []Result
}

func newRig(t *testing.T) *rig {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(i/4%251), 90, 160, 255
	}
	display := platform.Display{ID: 3, Bounds: platform.Rect{W: 800, H: 600}, Scale: 2, Primary: true}
	session := &capture.Session{Frames: []capture.Frame{capture.NewFrame(display, img)},
		Windows: []platform.Rect{{X: 500, Y: 400, W: 200, H: 150}}}
	r := &rig{t: t}
	r.c = NewController(session, sui.Light(), func(res Result) { r.results = append(r.results, res) })
	r.canvas = new(vui.Canvas)
	d, err := app.NewDriver(r.c.Config(0), r.canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	r.d = d
	return r
}

func (r *rig) must(err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) drag(x0, y0, x1, y1 float32) {
	r.t.Helper()
	r.must(r.d.Press(x0, y0, vui.PointerButtonPrimary))
	r.must(r.d.Move((x0+x1)/2, (y0+y1)/2))
	r.must(r.d.Move(x1, y1))
	r.must(r.d.Release(x1, y1, vui.PointerButtonPrimary))
}

func (r *rig) hasLabel(want string) bool {
	for _, l := range r.d.Snapshot().Labels {
		if strings.Contains(l.Value, want) {
			return true
		}
	}
	return false
}

func (r *rig) hit(id vui.ID) (vui.Hit, bool) {
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == id {
			return h, true
		}
	}
	return vui.Hit{}, false
}

func (r *rig) hitLabelled(label string) (vui.Hit, bool) {
	for _, h := range r.d.Snapshot().Hits {
		if h.Label == label {
			return h, true
		}
	}
	return vui.Hit{}, false
}

func (r *rig) click(id vui.ID) {
	r.t.Helper()
	h, ok := r.hit(id)
	if !ok {
		r.t.Fatalf("no control %s on screen", sui.Tree.Text(id))
	}
	r.must(r.d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
}

func (r *rig) state() *State { return r.c.States[0] }

func (r *rig) toolbarHits() int {
	n := 0
	for _, h := range r.d.Snapshot().Hits {
		if sui.IsToolbar(h.ID) {
			n++
		}
	}
	return n
}

func TestInitialHint(t *testing.T) {
	r := newRig(t)
	if !r.hasLabel("拖动选择区域") {
		t.Fatal("the hint capsule is shown at first")
	}
	if r.toolbarHits() != 0 {
		t.Fatal("no toolbar before a selection")
	}
	// The capsule fades after two seconds and the overlay then rests.
	r.must(r.d.Advance(2500 * 1e6))
	if r.hasLabel("拖动选择区域") {
		t.Fatal("hint still shown after 2.5 s")
	}
	if r.d.Transitioning() {
		t.Fatal("an idle overlay keeps drawing")
	}
}

func TestDragShowsSizeAndToolbar(t *testing.T) {
	r := newRig(t)
	r.must(r.d.Press(100, 100, vui.PointerButtonPrimary))
	r.must(r.d.Move(400, 300))
	if !r.hasLabel("600 × 400") {
		t.Fatal("size label during the drag shows physical pixels")
	}
	if r.toolbarHits() != 0 {
		t.Fatal("no toolbar while dragging")
	}
	r.must(r.d.Release(400, 300, vui.PointerButtonPrimary))
	for _, label := range []string{"完成", "保存", "取消", "矩形", "撤销", "钉住"} {
		if _, ok := r.hitLabelled(label); !ok {
			t.Fatalf("toolbar has no %q", label)
		}
	}
	if r.state().Phase != PhaseEditing {
		t.Fatal("editing after release")
	}
	// The toolbar is right-aligned below the selection.
	done, _ := r.hit(sui.ButtonID(sui.ActDone))
	if done.Bounds.Y < 300 || done.Bounds.X+done.Bounds.Width > 400 {
		t.Fatalf("toolbar placement %+v", done.Bounds)
	}
}

func TestResizeAndMove(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.drag(400, 300, 500, 350) // bottom-right handle
	if !r.hasLabel("800 × 500") {
		t.Fatalf("resize: sel = %+v", r.state().Sel)
	}
	r.drag(200, 200, 250, 220) // inside: move
	sel := r.state().Sel
	if sel.X != 150 || sel.Y != 120 || sel.Width != 400 || sel.Height != 250 {
		t.Fatalf("move: sel = %+v", sel)
	}
	r.drag(300, 300, 900, 900) // far past the corner: stays on screen
	sel = r.state().Sel
	if sel.X+sel.Width != 800 || sel.Y+sel.Height != 600 || sel.Width != 400 {
		t.Fatalf("clamped move: sel = %+v", sel)
	}
	r.must(r.d.Key(vui.KeyLeft, vui.ModifierShift))
	if r.state().Sel.X != 390 {
		t.Fatalf("Shift+Left moves 10: %+v", r.state().Sel)
	}
}

func TestRectangleUndoRedo(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.click(sui.ButtonID(sui.ActRect))
	if r.state().Doc.Tool != 1 {
		t.Fatal("the rectangle tool is selected")
	}
	if h, _ := r.hit(sui.ButtonID(sui.ActRect)); !h.Selected {
		t.Fatal("the tool button shows as selected")
	}
	if _, ok := r.hitLabelled("红"); !ok {
		t.Fatal("the property bar appears with a tool")
	}
	r.drag(150, 150, 250, 220)
	if len(r.state().Doc.Shapes) != 1 {
		t.Fatalf("shapes = %d", len(r.state().Doc.Shapes))
	}
	if h, _ := r.hit(sui.ButtonID(sui.ActUndo)); h.Disabled {
		t.Fatal("undo enabled after drawing")
	}
	if s := r.state().Doc.Shapes[0]; s.Points[0] != image.Pt(300, 300) || s.Points[1] != image.Pt(500, 440) {
		t.Fatalf("shape stored in image pixels: %v", s.Points)
	}
	if handled, err := r.d.ShortcutChord(vui.ModifierMeta, 'z'); err != nil || !handled || len(r.state().Doc.Shapes) != 0 {
		t.Fatalf("undo: %v %v %d", handled, err, len(r.state().Doc.Shapes))
	}
	if handled, err := r.d.ShortcutChord(vui.ModifierMeta|vui.ModifierShift, 'z'); err != nil || !handled || len(r.state().Doc.Shapes) != 1 {
		t.Fatalf("redo: %v %v", handled, err)
	}
	if handled, _ := r.d.ShortcutChord(vui.ModifierControl, 'y'); !handled {
		t.Fatal("Ctrl+Y")
	}
	// A colour from the property bar applies to the next shape.
	h, _ := r.hitLabelled("蓝")
	r.must(r.d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
	if r.state().Doc.Color.B != 0xFA {
		t.Fatalf("colour = %+v", r.state().Doc.Color)
	}
	// Cmd+1 toggles the rectangle tool off again (the digit alone does not reach the app).
	r.d.ShortcutChord(vui.ModifierMeta, '1')
	if r.state().Doc.Tool != 0 {
		t.Fatal("Cmd+1 toggles the tool")
	}
}

func TestEnterFinishes(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.click(sui.ButtonID(sui.ActArrow))
	r.drag(120, 120, 300, 250)
	r.must(r.d.Key(vui.KeyEnter, 0))
	if len(r.results) != 1 {
		t.Fatalf("results = %v", r.results)
	}
	res := r.results[0]
	if res.Kind != Done || res.Sel != image.Rect(200, 200, 800, 600) || len(res.Shapes) != 1 || res.Frame == nil {
		t.Fatalf("result = %+v", res)
	}
	r.must(r.d.Key(vui.KeyEnter, 0))
	if len(r.results) != 1 {
		t.Fatal("a session ends once")
	}
}

func TestEscape(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.click(sui.ButtonID(sui.ActPen))
	r.must(r.d.Press(150, 150, vui.PointerButtonPrimary))
	r.must(r.d.Move(200, 200))
	r.must(r.d.Key(vui.KeyEscape, 0))
	if len(r.results) != 0 || r.state().Doc.Draft != nil {
		t.Fatal("Escape mid-stroke cancels only the stroke")
	}
	r.must(r.d.Release(200, 200, vui.PointerButtonPrimary))
	if len(r.state().Doc.Shapes) != 0 {
		t.Fatal("the cancelled stroke is not committed")
	}
	r.must(r.d.Key(vui.KeyEscape, 0))
	if len(r.results) != 1 || r.results[0].Kind != Cancel {
		t.Fatalf("results = %+v", r.results)
	}
}

func TestSecondaryClickAndDoubleClick(t *testing.T) {
	r := newRig(t)
	r.must(r.d.ClickButton(50, 50, vui.PointerButtonSecondary))
	if len(r.results) != 1 || r.results[0].Kind != Cancel {
		t.Fatal("a right click with nothing selected cancels")
	}
	r = newRig(t)
	r.drag(100, 100, 400, 300)
	r.must(r.d.DoubleClick(200, 200))
	if len(r.results) != 1 || r.results[0].Kind != Done {
		t.Fatalf("double click: %+v", r.results)
	}
}

func TestSaveShortcutAndButton(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	if handled, _ := r.d.ShortcutChord(vui.ModifierMeta, 's'); !handled || len(r.results) != 1 || r.results[0].Kind != Save {
		t.Fatalf("Cmd+S: %+v", r.results)
	}
}

func TestToolbarPlacementInWindow(t *testing.T) {
	r := newRig(t)
	r.drag(100, 450, 400, 590) // near the bottom
	done, _ := r.hit(sui.ButtonID(sui.ActDone))
	if done.Bounds.Y+done.Bounds.Height > 450 {
		t.Fatalf("toolbar should be above the selection: %+v", done.Bounds)
	}
	r = newRig(t)
	r.drag(0, 0, 800, 600) // the whole screen
	done, _ = r.hit(sui.ButtonID(sui.ActDone))
	if done.Bounds.Y < 500 || done.Bounds.Y+done.Bounds.Height > 600 {
		t.Fatalf("toolbar should be inside the bottom: %+v", done.Bounds)
	}
}

// The pin button ends the session with the selection and its annotations,
// in image pixels and in window pixels, so the shell can pin the composed
// picture exactly where the selection was.
func TestPinButton(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.click(sui.ButtonID(sui.ActRect))
	r.drag(150, 150, 250, 220)
	h, ok := r.hit(sui.ButtonID(sui.ActPin))
	if !ok || h.Label != "钉住" {
		t.Fatal("the toolbar has a pin button")
	}
	r.click(sui.ButtonID(sui.ActPin))
	if len(r.results) != 1 {
		t.Fatalf("results = %+v", r.results)
	}
	res := r.results[0]
	if res.Kind != Pin || res.Sel != image.Rect(200, 200, 800, 600) || len(res.Shapes) != 1 || res.Frame == nil {
		t.Fatalf("pin result = %+v", res)
	}
	if res.SelWindow != (vui.Rect{X: 100, Y: 100, Width: 300, Height: 200}) {
		t.Fatalf("pin window rectangle = %+v", res.SelWindow)
	}
	if got := res.Frame.ScreenRect(res.SelWindow); got != (platform.Rect{X: 100, Y: 100, W: 300, H: 200}) {
		t.Fatalf("pin screen rectangle = %+v", got)
	}
}

func TestWindowSnapAndTinySelection(t *testing.T) {
	r := newRig(t)
	r.must(r.d.Move(600, 450))
	if !r.state().HasSnap {
		t.Fatal("the window under the pointer is a snap candidate")
	}
	r.must(r.d.Click(600, 450))
	if s := r.state().Sel; r.state().Phase != PhaseEditing || s.X != 500 || s.Width != 200 {
		t.Fatalf("a click selects the window: %+v", s)
	}
	r = newRig(t)
	r.drag(10, 10, 12, 12)
	if r.state().Phase != PhaseIdle {
		t.Fatal("a tiny drag returns to idle")
	}
}

func TestMagnifierColourCopy(t *testing.T) {
	r := newRig(t)
	r.must(r.d.Move(10, 10))
	if r.state().pixel == "" || !r.hasLabel("#") {
		t.Fatal("the magnifier shows the colour under the pointer")
	}
	if handled, _ := r.d.ShortcutChord(vui.ModifierMeta|vui.ModifierShift, 'c'); !handled || !strings.HasPrefix(r.d.Clipboard(), "#") {
		t.Fatalf("colour copy: %q", r.d.Clipboard())
	}
}

func TestPlaceToolbar(t *testing.T) {
	screen := vui.Size{Width: 1000, Height: 800}
	bar := vui.Size{Width: 300, Height: 40}
	r, p := PlaceToolbar(vui.Rect{X: 100, Y: 100, Width: 400, Height: 300}, bar, screen, 0)
	if p != PlaceBelow || r.Y != 408 || r.X != 200 {
		t.Fatalf("below: %+v %v", r, p)
	}
	r, p = PlaceToolbar(vui.Rect{X: 100, Y: 500, Width: 400, Height: 280}, bar, screen, 0)
	if p != PlaceAbove || r.Y != 452 {
		t.Fatalf("above: %+v %v", r, p)
	}
	r, p = PlaceToolbar(vui.Rect{Width: 1000, Height: 800}, bar, screen, 0)
	if p != PlaceInside || r.Y != 752 || r.X != 700 {
		t.Fatalf("inside: %+v %v", r, p)
	}
	r, _ = PlaceToolbar(vui.Rect{X: 0, Y: 100, Width: 100, Height: 100}, bar, screen, 0)
	if r.X != 0 {
		t.Fatalf("clamped to the left edge: %+v", r)
	}
	r, _ = PlaceToolbar(vui.Rect{X: 900, Y: 100, Width: 300, Height: 100}, bar, screen, 0)
	if r.X != 700 {
		t.Fatalf("clamped to the right edge: %+v", r)
	}
	// Room for the property bar is part of the decision.
	_, p = PlaceToolbar(vui.Rect{X: 100, Y: 100, Width: 400, Height: 630}, bar, screen, 42)
	if p != PlaceAbove {
		t.Fatalf("property bar would not fit below: %v", p)
	}
}

func BenchmarkPaintEditing(b *testing.B) {
	t := &testing.T{}
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	r.click(sui.ButtonID(sui.ActRect))
	r.drag(150, 150, 250, 220)
	// Each move of an in-progress rectangle repaints the whole overlay.
	r.must(r.d.Press(160, 160, vui.PointerButtonPrimary))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := r.d.Move(float32(200+i%50), 210); err != nil {
			b.Fatal(err)
		}
	}
}

// Viva paints a layer's images after its rectangles: the frozen picture must
// be the backdrop, or it covers the mask, the selection and the toolbar (the
// overlay looked like a plain frozen screen on a real Mac).
func TestFrozenPictureIsBackdrop(t *testing.T) {
	r := newRig(t)
	r.drag(100, 100, 400, 300)
	id := r.c.States[0].Frame.Bitmap.ID
	found := false
	for _, img := range r.canvas.Images() {
		if img.Bitmap.ID == id {
			found = true
			if !img.Backdrop {
				t.Fatal("the frozen picture is drawn above the mask and the toolbar")
			}
		}
	}
	if !found {
		t.Fatal("frozen picture not painted")
	}
	if len(r.canvas.Rectangles()) == 0 {
		t.Fatal("no mask or selection rectangles")
	}
}
