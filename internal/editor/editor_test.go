package editor

import (
	"image"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	sui "snaplite/internal/ui"
)

type rig struct {
	t      *testing.T
	e      *Editor
	config app.Config
	canvas *vui.Canvas
	d      *app.Driver
}

func newRig(t *testing.T, w, h int) *rig {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	r := &rig{t: t, e: New(img, 1, sui.Light()), canvas: &vui.Canvas{}}
	r.config = r.e.Config(800, 600)
	d, err := app.NewDriver(r.config, r.canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	r.d = d
	return r
}

func (r *rig) drawnTiles() map[uint64]bool {
	out := map[uint64]bool{}
	for _, img := range r.canvas.Images() {
		out[img.Bitmap.ID] = true
	}
	return out
}

func (r *rig) hasText(s string) bool {
	for _, l := range r.d.Snapshot().Texts {
		if strings.Contains(l.Value, s) {
			return true
		}
	}
	return false
}

func (r *rig) click(id vui.ID) {
	r.t.Helper()
	hits := r.d.Snapshot().Hits
	for i := len(hits) - 1; i >= 0; i-- {
		if h := hits[i]; h.ID == id {
			if err := r.d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
				r.t.Fatal(err)
			}
			return
		}
	}
	r.t.Fatalf("no control %s", sui.Tree.Text(id))
}

func TestTilesAndScrolling(t *testing.T) {
	r := newRig(t, 1000, 9000)
	if z := r.e.Zoom(); z >= 1 || z < 0.7 {
		t.Fatalf("an 800-wide window shrinks a 1000-wide image: zoom %v", z)
	}
	ids := r.e.TileIDs()
	if len(ids) != 5 {
		t.Fatalf("9000 rows make 5 tiles of 2048: %d", len(ids))
	}
	drawn := r.drawnTiles()
	if !drawn[ids[0]] || drawn[ids[4]] {
		t.Fatal("at the top only the first tiles are drawn")
	}
	if err := r.d.Scroll(vui.ScrollEvent{X: 400, Y: 300, DeltaY: 100000}); err != nil {
		t.Fatal(err)
	}
	if r.e.ScrollY() != r.e.MaxScroll() || r.e.MaxScroll() <= 0 {
		t.Fatalf("scroll clamps to the bottom: %v of %v", r.e.ScrollY(), r.e.MaxScroll())
	}
	drawn = r.drawnTiles()
	if !drawn[ids[4]] || drawn[ids[0]] {
		t.Fatalf("at the bottom the last tile is drawn and the first is not: %v", drawn)
	}
	if !r.hasText("长截图 · 1000 × 9000") {
		t.Fatal("title")
	}
	if err := r.d.Resize(1400, 600); err != nil {
		t.Fatal(err)
	}
	if r.e.Zoom() != 1 {
		t.Fatal("a wide window shows the image at 100 %, never larger")
	}
}

func TestAnnotateAndConfirmClose(t *testing.T) {
	r := newRig(t, 1000, 3000)
	r.click(sui.ButtonID(sui.ActArrow))
	if r.e.Doc.Tool != annotate.ToolArrow {
		t.Fatal("arrow tool")
	}
	if err := r.d.Press(200, 200, vui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	r.d.Move(300, 260)
	r.d.Release(400, 300, vui.PointerButtonPrimary)
	if len(r.e.Doc.Shapes) != 1 {
		t.Fatalf("shapes = %d", len(r.e.Doc.Shapes))
	}
	// Shapes are stored in image pixels: the window point maps through the zoom.
	want := int((200-TitlebarH-topPadding)/r.e.Zoom() + 0.5)
	if p := r.e.Doc.Shapes[0].Points[0]; p.Y != want {
		t.Fatalf("image coordinates: %v", p)
	}
	closed := 0
	r.config.OnCloseRequest(func() { closed++ })
	if err := r.d.Pump(); err != nil {
		t.Fatal(err)
	}
	if closed != 0 || !r.e.Confirming() || !r.hasText(DiscardTitle) {
		t.Fatal("closing with unsaved annotations asks first")
	}
	r.click(confirmKeep)
	if r.e.Confirming() || closed != 0 {
		t.Fatal("keep editing")
	}
	r.config.OnCloseRequest(func() { closed++ })
	r.d.Pump()
	r.click(confirmLeave)
	if closed != 1 {
		t.Fatal("discard closes")
	}
}

func TestCopyMarksClean(t *testing.T) {
	r := newRig(t, 600, 1000)
	copied := 0
	r.e.OnCopy = func(img *image.RGBA, shapes []annotate.Shape) { copied += len(shapes) + 1 }
	r.e.Doc.Tool = annotate.ToolRect
	r.d.Press(100, 100, vui.PointerButtonPrimary)
	r.d.Move(200, 200)
	r.d.Release(200, 200, vui.PointerButtonPrimary)
	if handled, _ := r.d.ShortcutChord(vui.ModifierMeta, 'c'); !handled || copied != 2 {
		t.Fatalf("Cmd+C copies with the shape: %d", copied)
	}
	closed := false
	r.config.OnCloseRequest(func() { closed = true })
	if !closed {
		t.Fatal("after copying, closing does not ask")
	}
}

// Viva paints a layer's images after its rectangles and paths, so the tiles
// must sit on a lower layer than the annotations and the title bar.
func TestTilesBelowAnnotationsAndTitlebar(t *testing.T) {
	r := newRig(t, 1000, 3000)
	r.click(sui.ButtonID(sui.ActRect))
	if err := r.d.Press(200, 200, vui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	r.d.Move(300, 260)
	r.d.Release(400, 300, vui.PointerButtonPrimary)
	top := uint16(0)
	for _, img := range r.canvas.Images() {
		top = max(top, img.Layer)
	}
	above := false
	for _, tri := range r.canvas.Triangles() {
		if tri.Layer <= top {
			t.Fatalf("an annotation triangle is on layer %d, not above the tiles' %d", tri.Layer, top)
		}
		above = true
	}
	if !above {
		t.Fatal("annotation not painted")
	}
	for _, rect := range r.canvas.Rectangles() {
		if rect.Bounds.Y == 0 && rect.Bounds.Height == TitlebarH && rect.Layer <= top {
			t.Fatalf("the title bar is on layer %d, under the tiles", rect.Layer)
		}
	}
}
