package pin

import (
	"image"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// rig is a pin in a Driver window, with Move and the callbacks recorded.
type rig struct {
	t      *testing.T
	pin    *Pin
	d      *app.Driver
	canvas *vui.Canvas
	moves  []platform.Rect
	closes int
	copies int
	saves  int
}

func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(i), 120, 200, 255
	}
	return img
}

// newRig sets the pin up before the Driver's first paint.
func newRig(t *testing.T, p *Pin) *rig {
	t.Helper()
	r := &rig{t: t, pin: p, canvas: new(vui.Canvas)}
	p.Move = func(f platform.Rect) { r.moves = append(r.moves, f) }
	p.OnClose = func() { r.closes++ }
	p.OnCopy = func() { r.copies++ }
	p.OnSave = func() { r.saves++ }
	d, err := app.NewDriver(p.Config(), r.canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	r.d = d
	return r
}

func imageRig(t *testing.T) *rig {
	return newRig(t, NewImage(Title(1), picture(600, 400), platform.Rect{X: 100, Y: 80, W: 300, H: 200}, sui.Light()))
}

func (r *rig) must(err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) click(id vui.ID) {
	r.t.Helper()
	hits := r.d.Snapshot().Hits
	for i := len(hits) - 1; i >= 0; i-- {
		if h := hits[i]; h.ID == id {
			r.must(r.d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
			return
		}
	}
	r.t.Fatalf("no control %s", sui.Tree.Text(id))
}

func (r *rig) hasText(want string) bool {
	for _, l := range r.d.Snapshot().Texts {
		if strings.Contains(l.Value, want) {
			return true
		}
	}
	return false
}

func TestPinWindow(t *testing.T) {
	r := imageRig(t)
	c := r.pin.Config()
	if c.Title != "snaplite-pin-1" || c.WindowMode != app.WindowFrameless || c.Width != 300 || c.Height != 200 || *c.Origin != (vui.Point{X: 100, Y: 80}) {
		t.Fatalf("config = %+v", c)
	}
}

func TestPinDoubleClickCloses(t *testing.T) {
	r := imageRig(t)
	r.must(r.d.Click(150, 100))
	if r.closes != 0 {
		t.Fatal("a single click keeps the pin")
	}
	r.must(r.d.DoubleClick(150, 100))
	if r.closes != 1 {
		t.Fatalf("closes = %d after a double click", r.closes)
	}
}

func TestPinEscapeCloses(t *testing.T) {
	r := imageRig(t)
	r.must(r.d.Key(vui.KeyEscape, 0))
	if r.closes != 1 {
		t.Fatal("Escape closes the pin")
	}
}

// Dragging moves the window by the pointer's travel in screen coordinates;
// the window coordinates stay put in a Driver, as they would not on screen.
func TestPinDragMoves(t *testing.T) {
	r := imageRig(t)
	r.must(r.d.Press(50, 40, vui.PointerButtonPrimary))
	r.must(r.d.Move(70, 55))
	r.must(r.d.Move(20, 10))
	r.must(r.d.Release(20, 10, vui.PointerButtonPrimary))
	want := []platform.Rect{{X: 120, Y: 95, W: 300, H: 200}, {X: 70, Y: 50, W: 300, H: 200}}
	if len(r.moves) != 2 || r.moves[0] != want[0] || r.moves[1] != want[1] {
		t.Fatalf("moves = %+v, want %+v", r.moves, want)
	}
	if r.pin.Frame() != want[1] {
		t.Fatalf("frame = %+v", r.pin.Frame())
	}
	r.must(r.d.Move(200, 150))
	if len(r.moves) != 2 {
		t.Fatal("moving without a press does not drag")
	}
	if r.closes != 0 {
		t.Fatal("a drag is not a close")
	}
}

func TestPinContextMenu(t *testing.T) {
	r := imageRig(t)
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	for _, want := range []string{"复制", "保存到本地…", "取消钉住"} {
		if !r.hasText(want) {
			t.Fatalf("menu lacks %q", want)
		}
	}
	if len(r.moves) != 0 {
		t.Fatal("a pin large enough for the menu is not enlarged")
	}
	r.click(menuCopy)
	if r.copies != 1 || r.pin.menu.IsOpen() {
		t.Fatalf("复制: copies %d, open %v", r.copies, r.pin.menu.IsOpen())
	}
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	r.click(menuSave)
	if r.saves != 1 {
		t.Fatal("保存到本地… asks the shell to save")
	}
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	r.must(r.d.Key(vui.KeyEscape, 0))
	if r.pin.menu.IsOpen() || r.closes != 0 {
		t.Fatalf("Escape closes the menu, not the pin: open %v closes %d", r.pin.menu.IsOpen(), r.closes)
	}
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	r.must(r.d.Click(280, 190)) // outside the menu
	if r.pin.menu.IsOpen() || r.closes != 0 {
		t.Fatal("a press outside closes the menu only")
	}
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	r.click(menuUnpin)
	if r.closes != 1 {
		t.Fatal("取消钉住 closes the pin")
	}
}

// A pin smaller than the menu grows while the menu is open and shrinks back.
func TestPinSmallGrowsForMenu(t *testing.T) {
	r := newRig(t, NewImage(Title(2), picture(80, 40), platform.Rect{X: 10, Y: 20, W: 40, H: 20}, sui.Light()))
	r.must(r.d.ClickButton(5, 5, vui.PointerButtonSecondary))
	if len(r.moves) != 1 || r.moves[0].W < menuWidth || r.moves[0].H < menuHeight || r.moves[0].X != 10 || r.moves[0].Y != 20 {
		t.Fatalf("grow: %+v", r.moves)
	}
	r.must(r.d.Resize(int(r.moves[0].W), int(r.moves[0].H)))
	if !r.hasText("取消钉住") {
		t.Fatal("menu shown in the enlarged window")
	}
	r.click(menuCopy)
	if len(r.moves) != 2 || r.moves[1] != (platform.Rect{X: 10, Y: 20, W: 40, H: 20}) {
		t.Fatalf("shrink: %+v", r.moves)
	}
}

// Viva paints a layer's images after its rectangles: the border (and the
// menu) must be on higher layers than the picture, or it covers them.
func TestPinBorderAbovePicture(t *testing.T) {
	r := imageRig(t)
	r.must(r.d.ClickButton(60, 50, vui.PointerButtonSecondary))
	var picture *vui.Image
	for i, img := range r.canvas.Images() {
		if img.Bitmap.ID == r.pin.bitmap.ID {
			picture = &r.canvas.Images()[i]
		}
	}
	if picture == nil || picture.Bounds != (vui.Rect{Width: 300, Height: 200}) {
		t.Fatalf("picture not painted over the window: %+v", picture)
	}
	border := 0
	for _, rect := range r.canvas.Rectangles() {
		if rect.Color == sui.Light().SurfaceBorder {
			border++
		}
		overlaps := rect.Bounds.X < picture.Bounds.X+picture.Bounds.Width && rect.Bounds.Y < picture.Bounds.Y+picture.Bounds.Height &&
			rect.Bounds.X+rect.Bounds.Width > picture.Bounds.X && rect.Bounds.Y+rect.Bounds.Height > picture.Bounds.Y
		if overlaps && rect.Layer <= picture.Layer && !picture.Backdrop {
			t.Fatalf("rectangle %+v is under the picture (layer %d)", rect, picture.Layer)
		}
	}
	if border < 4 {
		t.Fatalf("border rectangles = %d", border)
	}
}

func TestTextLayout(t *testing.T) {
	var measure vui.Canvas
	screen := platform.Rect{W: 1440, H: 900}
	lines, w, h := TextLayout(&measure, "hello", screen)
	if len(lines) != 1 || lines[0] != "hello" || w > TextMaxWidth || w < 2*TextPadding || h != textLine+2*TextPadding {
		t.Fatalf("short text: %q %v×%v", lines, w, h)
	}
	long := strings.Repeat("剪切板里的一段很长的文字，", 40)
	lines, w, h = TextLayout(&measure, long, screen)
	if len(lines) < 2 || w > TextMaxWidth || w < TextMaxWidth-2*sui.BodyTextSize {
		t.Fatalf("long text wraps at the maximum width: %d lines, w %v", len(lines), w)
	}
	for _, line := range lines {
		if measure.TextWidthWeight(line, sui.BodyTextSize, vui.WeightRegular) > TextMaxWidth-2*TextPadding {
			t.Fatalf("line %q is wider than the card", line)
		}
	}
	tall := strings.Repeat("一行\n", 200)
	lines, _, h = TextLayout(&measure, tall, screen)
	if h > screen.H*TextScreenShare || !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Fatalf("tall text: h %v, last %q", h, lines[len(lines)-1])
	}
}

func TestTextPinPaints(t *testing.T) {
	var measure vui.Canvas
	lines, w, h := TextLayout(&measure, "第一行\n第二行", platform.Rect{W: 1440, H: 900})
	r := newRig(t, NewText(Title(3), "第一行\n第二行", lines, platform.Rect{X: 5, Y: 5, W: w, H: h}, sui.Dark()))
	if !r.hasText("第一行") || !r.hasText("第二行") {
		t.Fatal("text lines painted")
	}
	r.must(r.d.DoubleClick(10, 10))
	if r.closes != 1 {
		t.Fatal("a double click closes a text pin too")
	}
}

func TestImageSizeAndPlacement(t *testing.T) {
	screen := platform.Rect{X: 0, Y: 0, W: 1000, H: 800}
	if w, h := ImageSize(picture(600, 400), 2, screen); w != 300 || h != 200 {
		t.Fatalf("natural size at 2× = %v×%v", w, h)
	}
	// 1600×400 at 1× is wider than 80% of the screen: shrunk to 800 wide.
	if w, h := ImageSize(picture(1600, 400), 1, screen); w != 800 || h != 200 {
		t.Fatalf("fitted size = %v×%v", w, h)
	}
	if r := Within(platform.Rect{X: 10, Y: 20, W: 300, H: 200}, 300, 200); r != (platform.Rect{X: 10, Y: 20, W: 300, H: 200}) {
		t.Fatalf("Within = %+v", r)
	}
	second := platform.Rect{X: 1000, Y: 0, W: 1000, H: 800}
	if r := Centered(second, 200, 100, 0); r != (platform.Rect{X: 1400, Y: 350, W: 200, H: 100}) {
		t.Fatalf("Centered = %+v", r)
	}
	if r := Centered(second, 200, 100, 2); r.X != 1448 || r.Y != 398 {
		t.Fatalf("third pin is offset by 48: %+v", r)
	}
	// The offset starts over before a pin would leave the screen.
	if r := Centered(second, 960, 760, 5); r.X+r.W > second.X+second.W || r.Y+r.H > 800 {
		t.Fatalf("cascade left the screen: %+v", r)
	}
}
