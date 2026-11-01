package history

import (
	"testing"

	vui "github.com/vivaui/viva/ui"

	sui "snaplite/internal/ui"
)

// menuRig is a panel with every shell callback, recording what they get.
type menuRig struct {
	*panelRig
	pastes int
	pinned []Item
	saved  []Item
}

func newMenuRig(t *testing.T, paste bool) *menuRig {
	m := &menuRig{}
	m.panelRig = newPanelRigSetup(t, three, func(p *Panel) {
		if paste {
			p.OnPaste = func() { m.pastes++ }
		}
		p.OnPin = func(it Item) { m.pinned = append(m.pinned, it) }
		p.OnSave = func(it Item) { m.saved = append(m.saved, it) }
	})
	return m
}

// rightClick presses the secondary button on row i and reports where.
func (r *panelRig) rightClick(i int) vui.Point {
	r.t.Helper()
	return r.rightClickAt(i, 30)
}

// rightClickAt presses the secondary button x points into row i.
func (r *panelRig) rightClickAt(i int, x float32) vui.Point {
	r.t.Helper()
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == rows.At(i) {
			at := vui.Point{X: h.Bounds.X + x, Y: h.Bounds.Y + h.Bounds.Height/2}
			if err := r.d.ClickButton(at.X, at.Y, vui.PointerButtonSecondary); err != nil {
				r.t.Fatal(err)
			}
			return at
		}
	}
	r.t.Fatalf("no row %d", i)
	return vui.Point{}
}

func (r *panelRig) hitBounds(id vui.ID) (vui.Rect, bool) {
	hits := r.d.Snapshot().Hits
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].ID == id {
			return hits[i].Bounds, true
		}
	}
	return vui.Rect{}, false
}

func TestPanelContextMenuItems(t *testing.T) {
	r := newMenuRig(t, true)
	at := r.rightClick(1)
	if !r.panel.menu.IsOpen() || r.panel.selected != 1 {
		t.Fatal("a secondary press opens the menu on that row and selects it")
	}
	for _, want := range []string{"复制", "粘贴", "钉到桌面", "保存到本地…", "删除"} {
		if !r.has(want) {
			t.Errorf("menu lacks %q", want)
		}
	}
	first, ok := r.hitBounds(menuCopy)
	if !ok || first.X < at.X-8 || first.Y < at.Y-8 || first.Y > at.Y+8 {
		t.Fatalf("menu not at the pointer %+v: first item %+v", at, first)
	}
	r.click(menuPin)
	if len(r.pinned) != 1 || r.pinned[0].Text != r.store.Items()[1].Text || r.panel.menu.IsOpen() {
		t.Fatalf("钉到桌面: %+v", r.pinned)
	}
	r.rightClick(2)
	r.click(menuSave)
	if len(r.saved) != 1 || r.saved[0].Kind != KindImage {
		t.Fatalf("保存到本地…: %+v", r.saved)
	}
	r.rightClick(0)
	r.click(menuPaste)
	if r.pastes != 1 || r.clip.text == "" {
		t.Fatalf("粘贴: pastes %d clip %q", r.pastes, r.clip.text)
	}
}

func TestPanelContextMenuWithoutPaste(t *testing.T) {
	r := newMenuRig(t, false)
	r.rightClick(0)
	if _, ok := r.hitBounds(menuPaste); ok || r.has("粘贴") {
		t.Fatal("no 粘贴 without OnPaste")
	}
	r.click(menuCopy)
	if r.closes != 1 || r.clip.text == "" {
		t.Fatalf("复制 copies and closes: closes %d", r.closes)
	}
}

func TestPanelContextMenuDelete(t *testing.T) {
	r := newMenuRig(t, true)
	victim := r.store.Items()[1].ID
	r.rightClick(1)
	r.click(menuDelete)
	if len(r.store.Items()) != 2 || r.rowCount() != 2 {
		t.Fatalf("删除: %d items, %d rows", len(r.store.Items()), r.rowCount())
	}
	for _, it := range r.store.Items() {
		if it.ID == victim {
			t.Fatal("the wrong item was deleted")
		}
	}
}

func TestPanelContextMenuDismiss(t *testing.T) {
	r := newMenuRig(t, true)
	r.rightClick(0)
	if err := r.d.Key(vui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if r.panel.menu.IsOpen() || r.closes != 0 {
		t.Fatalf("Escape closes the menu, not the panel: open %v closes %d", r.panel.menu.IsOpen(), r.closes)
	}
	r.rightClick(0)
	if err := r.d.Click(PanelWidth-10, PanelHeight-60); err != nil {
		t.Fatal(err)
	}
	if r.panel.menu.IsOpen() || r.clip.text != "" || r.closes != 0 {
		t.Fatal("a press outside closes the menu and does nothing else")
	}
	// A secondary press on another row while the menu is open moves the menu
	// there (beside the menu, which would otherwise take the press).
	r.rightClick(0)
	r.rightClickAt(2, PanelWidth-80)
	if !r.panel.menu.IsOpen() || r.panel.menuRow != 2 {
		t.Fatalf("menu row = %d, open %v", r.panel.menuRow, r.panel.menu.IsOpen())
	}
	if err := r.d.Key(vui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.d.Key(vui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if r.closes != 1 {
		t.Fatal("with the menu gone, Escape closes the panel again")
	}
}

// The menu near the panel's bottom-right corner is kept inside the panel.
func TestPanelContextMenuStaysInside(t *testing.T) {
	r := newMenuRig(t, true)
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == rows.At(2) {
			if err := r.d.ClickButton(PanelWidth-5, h.Bounds.Y+h.Bounds.Height-2, vui.PointerButtonSecondary); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []vui.ID{menuCopy, menuDelete} {
		b, ok := r.hitBounds(id)
		if !ok || b.X+b.Width > PanelWidth || b.Y+b.Height > PanelHeight || b.X < 0 || b.Y < 0 {
			t.Fatalf("menu item %s at %+v outside the panel", sui.Tree.Text(id), b)
		}
	}
}

func TestPanelRowPinButton(t *testing.T) {
	r := newMenuRig(t, true)
	if !r.hasHit(pinButtons.At(0)) || r.hasHit(pinButtons.At(1)) {
		t.Fatal("the selected row offers 钉住")
	}
	for _, id := range []vui.ID{copyButtons.At(0), pinButtons.At(0), pasteButtons.At(0)} {
		if b, ok := r.hitBounds(id); !ok || b.X+b.Width > PanelWidth-8 || b.Width < 30 {
			t.Fatalf("row button %s at %+v does not fit the row", sui.Tree.Text(id), b)
		}
	}
	r.hover(2)
	r.click(pinButtons.At(2))
	if len(r.pinned) != 1 || r.pinned[0].Kind != KindImage || r.closes != 0 {
		t.Fatalf("钉住 button: %+v closes %d", r.pinned, r.closes)
	}
	// Without OnPin there is no button.
	plain := newPanelRig(t, three)
	if plain.hasHit(pinButtons.At(0)) {
		t.Fatal("no 钉住 without OnPin")
	}
}
