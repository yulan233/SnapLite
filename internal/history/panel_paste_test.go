package history

import (
	"testing"

	vui "github.com/vivaui/viva/ui"
)

// pasteRig is a panel that can paste: OnPaste counts instead of closing.
func pasteRig(t *testing.T, clickPastes bool) (*panelRig, *int) {
	pastes := 0
	r := newPanelRigSetup(t, three, func(p *Panel) {
		p.OnPaste = func() { pastes++ }
		p.ClickPastes = clickPastes
	})
	return r, &pastes
}

// hover moves the pointer over row i.
func (r *panelRig) hover(i int) {
	r.t.Helper()
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == rows.At(i) {
			if err := r.d.Move(h.Bounds.X+20, h.Bounds.Y+h.Bounds.Height/2); err != nil {
				r.t.Fatal(err)
			}
			return
		}
	}
	r.t.Fatalf("no row %d", i)
}

func (r *panelRig) hasHit(id vui.ID) bool {
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == id {
			return true
		}
	}
	return false
}

func TestPanelSelectedRowOffersCopyAndPaste(t *testing.T) {
	r, pastes := pasteRig(t, false)
	if !r.hasHit(pasteButtons.At(0)) || r.hasHit(pasteButtons.At(1)) {
		t.Fatal("only the selected row shows its buttons")
	}
	r.hover(1)
	if !r.hasHit(pasteButtons.At(1)) {
		t.Fatal("the hovered row shows its buttons")
	}
	r.click(pasteButtons.At(1))
	if r.clip.text != "git commit -m \"fix: crash\"" || *pastes != 1 || r.closes != 0 {
		t.Fatalf("paste button: clip %q, pastes %d, closes %d", r.clip.text, *pastes, r.closes)
	}
	r2, pastes2 := pasteRig(t, false)
	r2.click(copyButtons.At(0))
	if r2.clip.text == "" || *pastes2 != 0 || r2.closes != 1 {
		t.Fatalf("copy button: clip %q, pastes %d, closes %d", r2.clip.text, *pastes2, r2.closes)
	}
}

func TestPanelCommandEnterPastes(t *testing.T) {
	r, pastes := pasteRig(t, false)
	if err := r.d.Key(vui.KeyEnter, vui.ModifierMeta); err != nil {
		t.Fatal(err)
	}
	if *pastes != 1 || r.closes != 0 {
		t.Fatalf("⌘↵ pastes: pastes %d closes %d", *pastes, r.closes)
	}
	r2, pastes2 := pasteRig(t, true)
	if err := r2.d.Key(vui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if *pastes2 != 1 {
		t.Fatal("with click-to-paste, Enter pastes")
	}
	r3, pastes3 := pasteRig(t, true)
	if err := r3.d.Key(vui.KeyEnter, vui.ModifierMeta); err != nil {
		t.Fatal(err)
	}
	if *pastes3 != 0 || r3.closes != 1 {
		t.Fatal("with click-to-paste, ⌘↵ only copies")
	}
}

func TestPanelNumberShortcutsPaste(t *testing.T) {
	r, pastes := pasteRig(t, false)
	if !r.has("⌘2") || !r.has("⌘3") {
		t.Fatal("rows show their quick-paste shortcuts")
	}
	if _, err := r.d.ShortcutChord(vui.ModifierMeta, '3'); err != nil {
		t.Fatal(err)
	}
	if len(r.clip.png) == 0 || *pastes != 1 {
		t.Fatalf("⌘3 pastes the third item (the image): png %d bytes, pastes %d", len(r.clip.png), *pastes)
	}
	if _, err := r.d.ShortcutChord(vui.ModifierMeta, '9'); err != nil {
		t.Fatal(err)
	}
	if *pastes != 1 {
		t.Fatal("⌘9 with three items does nothing")
	}
}

func TestPanelWindowsUsesControl(t *testing.T) {
	pastes := 0
	r := newPanelRigSetup(t, three, func(p *Panel) {
		p.OnPaste = func() { pastes++ }
		p.Mod = "Ctrl+"
	})
	if _, err := r.d.ShortcutChord(vui.ModifierMeta, '1'); err != nil {
		t.Fatal(err)
	}
	if pastes != 0 {
		t.Fatal("the Windows key is not the command key")
	}
	if _, err := r.d.ShortcutChord(vui.ModifierControl, '1'); err != nil {
		t.Fatal(err)
	}
	if pastes != 1 {
		t.Fatal("Ctrl+1 pastes on Windows")
	}
}

func TestPanelClickPastesWhenConfigured(t *testing.T) {
	r, pastes := pasteRig(t, true)
	r.click(rows.At(2))
	if *pastes != 1 || len(r.clip.png) == 0 {
		t.Fatal("a click pastes when configured so")
	}
}

// Restoring an item and the clipboard watcher then seeing that same content
// must not add a second item.
func TestRestoreThenWatcherDoesNotDuplicate(t *testing.T) {
	r := newPanelRig(t, three)
	r.click(rows.At(1))
	before := len(r.store.Items())
	if _, err := r.store.AddText(r.clip.text); err != nil {
		t.Fatal(err)
	}
	if got := len(r.store.Items()); got != before {
		t.Fatalf("items %d → %d after the same text came back", before, got)
	}
	if r.store.Items()[0].Text != r.clip.text {
		t.Fatal("the restored item is first")
	}
}

func TestSetLimitsEvictsAtOnce(t *testing.T) {
	s := open(t, t.TempDir(), Limits{MaxItems: 10, MaxBytes: 1 << 30})
	for _, text := range []string{"a", "b", "c", "d"} {
		s.AddText(text)
	}
	v := s.Version()
	s.SetLimits(Limits{MaxItems: 2, MaxBytes: 1 << 30})
	items := s.Items()
	if len(items) != 2 || items[0].Text != "d" || items[1].Text != "c" || s.Version() == v {
		t.Fatalf("after lowering the limit: %+v", items)
	}
}

func TestPanelFooterFits(t *testing.T) {
	for _, mod := range []string{"⌘", "Ctrl+"} {
		r := newPanelRigSetup(t, three, func(p *Panel) {
			p.OnPaste = func() {}
			p.Mod = mod
		})
		snap := r.d.Snapshot()
		var right float32
		for _, h := range snap.Hits {
			if h.ID == clearID {
				right = h.Bounds.X + h.Bounds.Width
			}
		}
		if right == 0 || right > PanelWidth {
			t.Fatalf("%s: the clear button ends at %v, outside %d", mod, right, PanelWidth)
		}
		if !r.has("粘贴") {
			t.Fatalf("%s: footer hint missing", mod)
		}
	}
}
