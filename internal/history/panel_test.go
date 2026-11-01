package history

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	sui "snaplite/internal/ui"
)

type panelRig struct {
	t      *testing.T
	store  *Store
	clip   *fakeClip
	panel  *Panel
	d      *app.Driver
	closes int
}

func newPanelRig(t *testing.T, fill func(*Store)) *panelRig {
	t.Helper()
	r := &panelRig{t: t, store: open(t, t.TempDir(), Limits{}), clip: &fakeClip{}}
	if fill != nil {
		fill(r.store)
	}
	r.panel = NewPanel(r.store, r.clip, sui.Light())
	r.panel.loadThumb = func(id string) { _, _ = r.store.LoadThumbnail(id) }
	r.panel.OnClose = func() { r.closes++ }
	r.panel.pending = map[string]bool{}
	r.panel.ensureThumbs()
	var canvas vui.Canvas
	d, err := app.NewDriver(r.panel.Config(app.StatusAnchor{}), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	r.d = d
	return r
}

func (r *panelRig) has(want string) bool {
	for _, l := range r.d.Snapshot().Labels {
		if strings.Contains(l.Value, want) {
			return true
		}
	}
	return false
}

func (r *panelRig) rowCount() int {
	n := 0
	for _, h := range r.d.Snapshot().Hits {
		if _, ok := h.ID.In(rows); ok {
			n++
		}
	}
	return n
}

func (r *panelRig) click(id vui.ID) {
	r.t.Helper()
	// The last hit with the id is the front-most: a dialog's cancel button
	// shares its identity with the scrim behind it.
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

func three(s *Store) {
	s.AddImage(picture(64, 32, 1))
	s.AddText("git commit -m \"fix: crash\"")
	s.AddText("这是一段从网页复制的文字\n第二行\n第三行")
}

func TestPanelEmptyState(t *testing.T) {
	r := newPanelRig(t, nil)
	if !r.has(EmptyText) || !r.has("共 0 条") || r.rowCount() != 0 {
		t.Fatal("empty state")
	}
}

func TestPanelRows(t *testing.T) {
	r := newPanelRig(t, three)
	if r.rowCount() != 3 || !r.has("共 3 条") {
		t.Fatalf("rows = %d", r.rowCount())
	}
	if !r.has("64 × 32 图片") || !r.has("刚刚") || !r.has("git commit") {
		t.Fatal("row contents")
	}
	if r.has("第三行") {
		t.Fatal("text rows show two lines at most")
	}
	if _, ok := r.store.Thumbnail(r.store.Items()[2].ID); !ok {
		t.Fatal("the visible image row has a thumbnail")
	}
	if r.d.Snapshot().Focused != searchID {
		t.Fatal("the search field has the keyboard when the panel opens")
	}
}

func TestPanelKeyboardRestore(t *testing.T) {
	r := newPanelRig(t, three)
	for _, k := range []vui.Key{vui.KeyDown, vui.KeyDown, vui.KeyEnter} {
		if err := r.d.Key(k, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.clip.png) == 0 || r.clip.text != "" {
		t.Fatalf("the third item (the image) is restored: %+v", r.clip)
	}
	if r.closes != 1 {
		t.Fatal("the panel asks to close after restoring")
	}
	if r.store.Items()[0].Kind != KindImage {
		t.Fatal("the restored item moves to the front")
	}
}

func TestPanelClickRestoreAndDelete(t *testing.T) {
	r := newPanelRig(t, three)
	r.click(rows.At(1))
	if r.clip.text != "git commit -m \"fix: crash\"" {
		t.Fatalf("click restores: %q", r.clip.text)
	}
	r = newPanelRig(t, three)
	if err := r.d.Key(vui.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.store.Items()) != 2 || r.rowCount() != 2 {
		t.Fatalf("Backspace in the empty search field deletes the selection: %d", len(r.store.Items()))
	}
}

func TestPanelSearch(t *testing.T) {
	r := newPanelRig(t, three)
	if err := r.d.Type("GIT"); err != nil {
		t.Fatal(err)
	}
	if r.rowCount() != 1 || !r.has("git commit") {
		t.Fatalf("search: %d rows", r.rowCount())
	}
	if err := r.d.Key(vui.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.store.Items()) != 3 {
		t.Fatal("Backspace with text in the field edits the text")
	}
	if err := r.d.Key(vui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.clip.text, "git") {
		t.Fatalf("Enter restores the match: %q", r.clip.text)
	}
}

func TestPanelClearConfirm(t *testing.T) {
	r := newPanelRig(t, three)
	r.click(clearID)
	if !r.has("清空剪切板历史？") || len(r.store.Items()) != 3 {
		t.Fatal("clearing asks first")
	}
	r.click(confirmCancel)
	if r.has("清空剪切板历史？") || len(r.store.Items()) != 3 {
		t.Fatal("cancel keeps everything")
	}
	r.click(clearID)
	r.click(confirmClear)
	if len(r.store.Items()) != 0 || !r.has(EmptyText) || !r.has("共 0 条") {
		t.Fatal("confirmed clear empties the history")
	}
}

func TestPanelEscapeAndWake(t *testing.T) {
	r := newPanelRig(t, three)
	if err := r.d.Key(vui.KeyEscape, 0); err != nil || r.closes != 1 {
		t.Fatal("Escape closes")
	}
	r.store.AddText("new from another app")
	r.panel.Wake()
	if err := r.d.Pump(); err != nil {
		t.Fatal(err)
	}
	if r.rowCount() != 4 {
		t.Fatalf("the panel follows the store: %d", r.rowCount())
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		now.Add(-20 * time.Second):                       "刚刚",
		now.Add(-3 * time.Minute):                        "3 分钟前",
		now.Add(-2 * time.Hour):                          "2 小时前",
		time.Date(2026, 10, 31, 23, 0, 0, 0, time.Local): "昨天",
		time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local):   "10月3日",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local):    "1月1日",
	}
	for when, want := range cases {
		if got := RelativeTime(now, when); got != want {
			t.Errorf("RelativeTime(%v) = %q want %q", when, got, want)
		}
	}
	early := time.Date(2026, 11, 1, 0, 30, 0, 0, time.Local)
	if got := RelativeTime(early, time.Date(2026, 10, 31, 23, 50, 0, 0, time.Local)); got != "40 分钟前" {
		t.Errorf("minutes win over yesterday: %q", got)
	}
}

func TestPanelTabReachesTheList(t *testing.T) {
	r := newPanelRig(t, three)
	if err := r.d.Key(vui.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if id := r.d.Snapshot().Focused; id != rows.At(0) {
		t.Fatalf("Tab moves from the search field to the selected row: %s", sui.Tree.Text(id))
	}
	r.d.Key(vui.KeyDown, 0)
	r.d.Key(vui.KeyEnter, 0)
	if r.clip.text != "git commit -m \"fix: crash\"" {
		t.Fatalf("Down and Enter from the list: %q", r.clip.text)
	}
	if err := r.d.Key(vui.KeyDelete, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.store.Items()) != 2 {
		t.Fatal("Delete with the list focused removes the selection")
	}
}
