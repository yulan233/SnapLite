package history

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	vui "github.com/vivaui/viva/ui"

	sui "snaplite/internal/ui"
)

var (
	panelFeature  = sui.Tree.Feature("history")
	searchID      = sui.Tree.ID(panelFeature, "search")
	listID        = sui.Tree.ID(panelFeature, "list")
	clearID       = sui.Tree.ID(panelFeature, "clear")
	rows          = sui.Tree.Sub(panelFeature, sui.Tree.Name("rows"))
	copyButtons   = sui.Tree.Sub(panelFeature, sui.Tree.Name("copy"))
	pasteButtons  = sui.Tree.Sub(panelFeature, sui.Tree.Name("paste"))
	pinButtons    = sui.Tree.Sub(panelFeature, sui.Tree.Name("pin"))
	menuScope     = sui.Tree.ID(panelFeature, "menu")
	menuDismiss   = sui.Tree.ID(panelFeature, "menu-dismiss")
	menuCopy      = sui.Tree.ID(panelFeature, "menu-copy")
	menuPaste     = sui.Tree.ID(panelFeature, "menu-paste")
	menuPin       = sui.Tree.ID(panelFeature, "menu-pin")
	menuSave      = sui.Tree.ID(panelFeature, "menu-save")
	menuDelete    = sui.Tree.ID(panelFeature, "menu-delete")
	confirmScope  = sui.Tree.Sub(panelFeature, sui.Tree.Name("confirm"))
	confirmFocus  = sui.Tree.ID(confirmScope, "scope")
	confirmCancel = sui.Tree.ID(confirmScope, "cancel")
	confirmClear  = sui.Tree.ID(confirmScope, "clear")
)

// Panel geometry (docs/02 §3.4).
const (
	PanelWidth     = 420
	PanelHeight    = 560
	imageRowHeight = 112
	textRowHeight  = 52
	thumbMaxWidth  = 160
	thumbMaxHeight = 96
	menuWidth      = 168
	menuLayer      = 20
)

// EmptyText is the empty state's message.
const EmptyText = "复制的图片和文字会出现在这里"

// row is the display form of one item, computed when the history changes so
// painting formats nothing.
type row struct {
	item    Item
	preview string // the first lines of a text, trimmed for display
	detail  string // "1280 × 720 图片" or "128 字"
	age     string // RelativeTime
	label   string // accessibility name
	lower   string // lower-case text for searching
}

// Panel is the clipboard-history window.
type Panel struct {
	store  *Store
	clip   Clipboard
	tokens sui.Tokens
	now    func() time.Time

	editor *text.Editor
	input  app.TextInput
	view   flex.View
	wake   app.Invalidator

	version  uint64
	all      []row
	shown    []int // indexes into all that match the query
	count    string
	query    string
	selected int
	confirm  bool
	closed   bool
	pending  map[string]bool // thumbnails being made
	// menu is the context menu, open on shown row menuRow at menuAt.
	menu    components.ContextMenuState
	menuRow int
	menuAt  vui.Point

	// OnClose asks the window to close (after a restore, Escape, or by the shell).
	OnClose func()
	// OnPaste runs instead of OnClose after an item was put on the clipboard to
	// be pasted: the shell closes the panel and pastes into the application
	// that was in front. Nil disables pasting.
	OnPaste func()
	// OnPin pins an item on the desktop and OnSave saves it to a file; the
	// shell closes the panel first. Nil leaves the action out.
	OnPin  func(Item)
	OnSave func(Item)
	// ClickPastes makes a click on an item (and Enter) paste it; otherwise
	// they copy it and Command/Control+Enter pastes.
	ClickPastes bool
	// Mod is how the platform's command key is written in hints: "⌘" or "Ctrl+".
	Mod string
	// thumbs is how thumbnails are made; tests run it inline.
	loadThumb func(id string)
}

// NewPanel makes the panel over a store. clip receives restored items.
func NewPanel(store *Store, clip Clipboard, tokens sui.Tokens) *Panel {
	p := &Panel{store: store, clip: clip, tokens: tokens, now: time.Now, editor: text.NewEditor(""), pending: map[string]bool{}, Mod: "⌘"}
	p.loadThumb = func(id string) {
		go func() {
			_, _ = store.LoadThumbnail(id)
			p.wake.Invalidate()
		}()
	}
	p.refresh()
	return p
}

// Wake asks the panel to repaint from another goroutine (the store changed).
func (p *Panel) Wake() { p.wake.Invalidate() }

// refresh rebuilds the rows when the store changed.
func (p *Panel) refresh() {
	v := p.store.Version()
	if p.all != nil && v == p.version {
		return
	}
	p.version = v
	now := p.now()
	items := p.store.Items()
	p.all = p.all[:0]
	for _, it := range items {
		r := row{item: it, age: RelativeTime(now, it.Created)}
		if it.Kind == KindImage {
			r.detail = fmt.Sprintf("%d × %d 图片", it.Size[0], it.Size[1])
			r.label = r.detail
		} else {
			r.preview = previewText(it.Text)
			r.detail = fmt.Sprintf("%d 字", utf8.RuneCountInString(it.Text))
			r.label = r.preview
			r.lower = strings.ToLower(it.Text)
		}
		p.all = append(p.all, r)
	}
	p.count = countText(len(p.all))
	p.filter()
}

// previewText is the start of a text: two lines at most, 200 runes at most.
func previewText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) > 2 {
		lines = lines[:2]
		lines[1] += "…"
	}
	s = strings.Join(lines, "\n")
	if utf8.RuneCountInString(s) > 200 {
		s = string([]rune(s)[:200]) + "…"
	}
	return s
}

// filter applies the query: text items containing it, case-insensitively.
// Images take no part in a search.
func (p *Panel) filter() {
	q := strings.ToLower(strings.TrimSpace(p.query))
	p.shown = p.shown[:0]
	for i, r := range p.all {
		if q == "" || (r.item.Kind == KindText && strings.Contains(r.lower, q)) {
			p.shown = append(p.shown, i)
		}
	}
	if p.selected >= len(p.shown) {
		p.selected = max(len(p.shown)-1, 0)
	}
	p.ensureThumbs()
}

// ensureThumbs starts thumbnail work for visible image rows.
func (p *Panel) ensureThumbs() {
	offset := p.view.ScrollState(listID).Offset
	y := float32(0)
	for _, idx := range p.shown {
		r := p.all[idx]
		h := rowHeight(r.item)
		if y+h >= offset && y <= offset+PanelHeight && r.item.Kind == KindImage && !p.pending[r.item.ID] {
			if _, ok := p.store.Thumbnail(r.item.ID); !ok {
				p.pending[r.item.ID] = true
				p.loadThumb(r.item.ID)
			}
		}
		y += h
	}
}

func rowHeight(it Item) float32 {
	if it.Kind == KindImage {
		return imageRowHeight
	}
	return textRowHeight
}

// Config is the panel window: a WindowPanel that closes when it loses focus.
func (p *Panel) Config(anchor app.StatusAnchor) app.Config {
	return app.Config{
		Title: "snaplite-history", WindowMode: app.WindowPanel, Anchor: anchor,
		Width: PanelWidth, Height: PanelHeight, Tree: &sui.Tree,
		Paint:       p.paint,
		OnPointer:   p.pointer,
		OnKey:       p.key,
		OnShortcut:  p.shortcut,
		OnScroll:    p.scroll,
		TextInput:   p.textInput,
		Invalidator: &p.wake,
		OnWake:      p.refresh,
		OnClosed:    func() { p.closed = true },
	}
}

func (p *Panel) close() {
	if p.OnClose != nil {
		p.OnClose()
	}
}

func (p *Panel) textInput(ctx app.TextInputContext) *app.TextInput {
	if ctx.ID != searchID {
		return nil
	}
	p.input.Editor = nil
	p.input.NavigationKeys = true
	p.input.State = app.TextStateOf(p.editor)
	p.input.OnEvent = p.textEvent
	return &p.input
}

// textEvent edits the search field; Delete or Backspace in an empty field
// deletes the selected item instead.
func (p *Panel) textEvent(e app.TextEvent) (app.TextState, error) {
	if e.Kind == app.TextCommandEvent && p.editor.RuneCount() == 0 &&
		(e.Command == app.TextDeleteBackward || e.Command == app.TextDeleteForward) {
		p.deleteSelected()
		return app.TextStateOf(p.editor), nil
	}
	e.Apply(p.editor)
	if q := p.editor.String(); q != p.query {
		p.query = q
		p.selected = 0
		p.filter()
	}
	return app.TextStateOf(p.editor), nil
}

// Selected returns the selected item, if any.
func (p *Panel) Selected() (Item, bool) {
	if p.selected < 0 || p.selected >= len(p.shown) {
		return Item{}, false
	}
	return p.all[p.shown[p.selected]].item, true
}

// restore puts shown item i on the clipboard, then closes the panel, or with
// paste set hands over to OnPaste to paste it where the user was.
func (p *Panel) restore(i int, paste bool) {
	if i < 0 || i >= len(p.shown) {
		return
	}
	if err := p.store.Restore(p.all[p.shown[i]].item.ID, p.clip); err != nil {
		return
	}
	if paste && p.OnPaste != nil {
		p.OnPaste()
		return
	}
	p.close()
}

// clickPastes is what a click or Enter does; the command key does the other.
func (p *Panel) clickPastes() bool { return p.ClickPastes && p.OnPaste != nil }

// commandKey is Command on macOS and Control on Windows, as Mod says.
func (p *Panel) commandKey(m vui.Modifiers) bool {
	if p.Mod == "⌘" {
		return m&vui.ModifierMeta != 0
	}
	return m&vui.ModifierControl != 0
}

// shortcut takes Command/Control+1..9: paste the first nine shown items.
func (p *Panel) shortcut(e vui.KeyEvent) vui.EventResult {
	if p.confirm || p.menu.IsOpen() || !p.commandKey(e.Modifiers) || len(e.Text) != 1 || e.Text[0] < '1' || e.Text[0] > '9' {
		return vui.EventResult{}
	}
	i := int(e.Text[0] - '1')
	if i < len(p.shown) {
		p.selected = i
		p.restore(i, p.OnPaste != nil)
	}
	return vui.EventResult{Handled: true, Repaint: true}
}

func (p *Panel) deleteSelected() {
	if it, ok := p.Selected(); ok {
		_ = p.store.Delete(it.ID)
		p.refresh()
	}
}

// scrollTo keeps row i inside the list's viewport.
func (p *Panel) scrollTo(i int) {
	state := p.view.ScrollState(listID)
	top := float32(0)
	for k := 0; k < i; k++ {
		top += rowHeight(p.all[p.shown[k]].item)
	}
	bottom := top + rowHeight(p.all[p.shown[i]].item)
	viewport := float32(PanelHeight - 52 - 36 - 2)
	if top < state.Offset {
		state.Offset = top
	} else if bottom > state.Offset+viewport {
		state.Offset = bottom - viewport
	}
}

func (p *Panel) key(e vui.KeyEvent) vui.EventResult {
	if p.confirm || p.menu.IsOpen() {
		// The dialog or the menu has the keyboard; Escape dismisses it.
		return vui.EventResult{}
	}
	switch e.Key {
	case vui.KeyDown, vui.KeyUp:
		if len(p.shown) == 0 {
			return vui.EventResult{Handled: true}
		}
		if e.Key == vui.KeyDown {
			p.selected = min(p.selected+1, len(p.shown)-1)
		} else {
			p.selected = max(p.selected-1, 0)
		}
		p.scrollTo(p.selected)
		p.ensureThumbs()
		return vui.EventResult{Handled: true, Repaint: true}
	case vui.KeyEnter:
		paste := p.clickPastes() != p.commandKey(e.Modifiers)
		p.restore(p.selected, paste && p.OnPaste != nil)
		return vui.EventResult{Handled: true, Repaint: true}
	case vui.KeyEscape:
		p.close()
		return vui.EventResult{Handled: true}
	case vui.KeyDelete, vui.KeyBackspace:
		if p.editor.RuneCount() == 0 {
			p.deleteSelected()
			return vui.EventResult{Handled: true, Repaint: true}
		}
	}
	return vui.EventResult{}
}

// rowTargets are the identities a press on row i may land on.
var rowTargets = [...]vui.Scope{rows, copyButtons, pasteButtons, pinButtons}

// pointer opens the context menu where a row is pressed with the secondary
// button. A press while a menu is open first dismisses it (the menu's
// backdrop passes secondary presses on), so the press opens a new one.
func (p *Panel) pointer(e vui.PointerEvent) vui.EventResult {
	if e.Kind != vui.PointerDown || e.Button != vui.PointerButtonSecondary || p.confirm || p.menu.IsOpen() {
		return vui.EventResult{}
	}
	for _, scope := range rowTargets {
		if i, ok := e.Target.In(scope); ok && i < len(p.shown) {
			p.selected = i
			p.menu, p.menuRow, p.menuAt = p.menu.Open(), i, vui.Point{X: e.X, Y: e.Y}
			return vui.EventResult{Handled: true, Repaint: true}
		}
	}
	return vui.EventResult{}
}

func (p *Panel) scroll(e vui.ScrollEvent) vui.EventResult {
	result := p.view.Wheel(e)
	p.ensureThumbs()
	return result
}

func (p *Panel) paint(canvas *vui.Canvas, size vui.Size) {
	t := p.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	canvas.Rounded(bounds, sui.RadiusSurface, t.Surface)
	if !p.menu.IsOpen() {
		canvas.RequestFocus(searchID)
	}
	p.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
		f.Column().Fill().Children(func() {
			f.Row().Height(52).PadX(12).Items(flex.Center).Children(func() {
				value := p.editor.String()
				focused := canvas.Focused() == searchID
				sel := p.editor.Selection()
				f.Input(components.InputSnapshot{
					ID: searchID, Label: "搜索剪切板历史", Value: value, Placeholder: "搜索剪切板历史",
					Focused: focused, PlatformEditing: focused && value != "", ShowCaret: focused && value == "",
					NavigationKeys: true, Caret: p.editor.Caret(), SelectionStart: sel.Start, SelectionEnd: sel.End,
				}).Grow(1)
			})
			f.Divider()
			if len(p.shown) == 0 {
				f.Column().Grow(1).Items(flex.Center).Justify(flex.Center).Gap(8).Children(func() {
					f.Text(EmptyText).Muted()
				})
			} else {
				f.ScrollArea(listID).Grow(1).Children(func() { p.paintRows(f, canvas) })
			}
			f.Divider()
			f.Row().Height(36).PadX(12).Gap(10).Items(flex.Center).Children(func() {
				f.Text(p.count).Muted().FontSize(sui.MutedTextSize)
				if hint := p.hint(); hint != "" {
					f.Text(hint).Muted().FontSize(sui.MutedTextSize)
				}
				f.Spacer()
				if f.Button(clearID, "清空历史").Ghost().Destructive().Small().Disabled(len(p.all) == 0).Clicked() {
					canvas.AfterPaint(func() { p.confirm = true })
				}
			})
		})
		if p.confirm {
			d := f.Dialog(flex.DialogSpec{
				Title: "清空剪切板历史？", Description: "所有记录将被删除，且无法恢复。", Width: 340,
				Scope: confirmFocus, DismissID: confirmCancel, NoClose: true,
			})
			d.Footer(func() {
				f.Button(confirmCancel, "取消").Outline()
				if f.Button(confirmClear, "清空").Destructive().Clicked() {
					canvas.AfterPaint(func() {
						_ = p.store.Clear()
						p.confirm = false
						p.refresh()
					})
				}
			})
			if d.Dismissed() {
				canvas.AfterPaint(func() { p.confirm = false })
			}
		}
	})
	if p.menu.IsOpen() {
		p.paintMenu(canvas, bounds)
	}
}

// menuItems is the context menu of a row: 复制, 粘贴, 钉到桌面 and 保存到本地…
// as far as the shell offers them, then 删除.
func (p *Panel) menuItems() []components.ContextMenuItem {
	items := []components.ContextMenuItem{{ID: menuCopy, Label: "复制"}}
	if p.OnPaste != nil {
		items = append(items, components.ContextMenuItem{ID: menuPaste, Label: "粘贴"})
	}
	if p.OnPin != nil {
		items = append(items, components.ContextMenuItem{ID: menuPin, Label: "钉到桌面"})
	}
	if p.OnSave != nil {
		items = append(items, components.ContextMenuItem{ID: menuSave, Label: "保存到本地…"})
	}
	return append(items, components.ContextMenuItem{ID: menuDelete, Label: "删除", Variant: components.ContextMenuItemDestructive})
}

// paintMenu draws the open context menu at the pointer, kept inside the panel;
// a press outside it or Escape closes it.
func (p *Panel) paintMenu(canvas *vui.Canvas, bounds vui.Rect) {
	items := p.menuItems()
	// The rows, the separator before 删除 and the surface's padding.
	height := float32(len(items))*32 + 9 + 2*4
	menu := components.BeginContextMenu(canvas, p.tokens.VivaTheme(), vui.Rect{X: p.menuAt.X, Y: p.menuAt.Y, Width: menuWidth, Height: height},
		components.ContextMenuOptions{Layer: menuLayer, Scope: menuScope, DismissID: menuDismiss, Viewport: bounds})
	for _, item := range items {
		if item.ID == menuDelete {
			menu.AddSeparator()
		}
		menu.AddItem(item)
	}
	menu.End()
	i := p.menuRow
	act := func(fn func()) {
		canvas.AfterPaint(func() {
			p.menu = p.menu.Close()
			if fn != nil && i < len(p.shown) {
				fn()
			}
		})
	}
	item := func() Item { return p.all[p.shown[i]].item }
	switch {
	case canvas.Clicked(menuCopy):
		act(func() { p.restore(i, false) })
	case canvas.Clicked(menuPaste):
		act(func() { p.restore(i, true) })
	case canvas.Clicked(menuPin):
		act(func() { p.OnPin(item()) })
	case canvas.Clicked(menuSave):
		act(func() { p.OnSave(item()) })
	case canvas.Clicked(menuDelete):
		act(func() {
			_ = p.store.Delete(item().ID)
			p.refresh()
		})
	case canvas.Clicked(menuDismiss):
		act(nil)
	}
}

// hint is the keyboard help in the footer: "↵ 复制 · ⌘↵ 粘贴". The rows show
// their own quick-paste chords (⌘1..⌘9).
func (p *Panel) hint() string {
	if p.OnPaste == nil {
		return "↵ 复制"
	}
	if p.clickPastes() {
		return fmt.Sprintf("↵ 粘贴 · %s↵ 复制", p.Mod)
	}
	return fmt.Sprintf("↵ 复制 · %s↵ 粘贴", p.Mod)
}

// rowActive reports whether row i shows its buttons: selected or hovered.
func (p *Panel) rowActive(canvas *vui.Canvas, i int) bool {
	h := canvas.Hovered()
	return i == p.selected || h == rows.At(i) || h == copyButtons.At(i) || h == pasteButtons.At(i) || h == pinButtons.At(i)
}

func (p *Panel) paintRows(f *flex.Frame, canvas *vui.Canvas) {
	t := p.tokens
	for i, idx := range p.shown {
		r := &p.all[idx]
		box := f.Row().Height(rowHeight(r.item)).PadX(12).Gap(12).Items(flex.Center).
			HoverBackground(t.Hover).HitWith(rows.At(i), r.label, vui.HitOptions{Selected: i == p.selected, SkipTab: i != p.selected})
		if i == p.selected {
			box = box.Background(t.Selected)
		}
		clicked := box.Children(func() {
			if r.item.Kind == KindImage {
				thumb, ok := p.store.Thumbnail(r.item.ID)
				w, h := thumbSize(r.item, thumb, ok)
				if ok {
					f.Image(thumb).Size(w, h).Radius(sui.RadiusSwatch)
				} else {
					f.Slot().Size(w, h).Background(t.Hover).Radius(sui.RadiusSwatch)
				}
				f.Text(r.detail).FontSize(sui.BodyTextSize).Grow(1)
			} else {
				f.Column().Grow(1).Gap(2).Children(func() {
					f.Paragraph(r.preview, 2).FontSize(sui.BodyTextSize)
				})
			}
			if p.rowActive(canvas, i) {
				// The selected or hovered row offers both actions.
				if f.Button(copyButtons.At(i), "复制").Outline().Small().Clicked() {
					canvas.AfterPaint(func() { p.selected = i; p.restore(i, false) })
				}
				if p.OnPin != nil && f.Button(pinButtons.At(i), "钉住").Outline().Small().Clicked() {
					canvas.AfterPaint(func() { p.selected = i; p.OnPin(r.item) })
				}
				if p.OnPaste != nil && f.Button(pasteButtons.At(i), "粘贴").Small().Clicked() {
					canvas.AfterPaint(func() { p.selected = i; p.restore(i, true) })
				}
				return
			}
			f.Column().Items(flex.End).Gap(2).Children(func() {
				f.Row().Gap(6).Children(func() {
					if i < 9 && p.OnPaste != nil {
						f.Text(fmt.Sprintf("%s%d", p.Mod, i+1)).Muted().FontSize(sui.MutedTextSize)
					}
					f.Text(r.age).Muted().FontSize(sui.MutedTextSize)
				})
				if r.item.Kind == KindText {
					f.Text(r.detail).Muted().FontSize(sui.MutedTextSize)
				}
			})
		}).Clicked()
		if clicked {
			canvas.AfterPaint(func() {
				p.selected = i
				p.restore(i, p.clickPastes())
			})
		}
	}
}

// thumbSize fits a thumbnail in 160×96 logical pixels.
func thumbSize(it Item, b vui.Bitmap, ok bool) (float32, float32) {
	w, h := float32(it.Size[0]), float32(it.Size[1])
	if ok {
		w, h = float32(b.Width), float32(b.Height)
	}
	if w <= 0 || h <= 0 {
		return thumbMaxHeight, thumbMaxHeight
	}
	scale := min(thumbMaxWidth/w, thumbMaxHeight/h, 1)
	if ok && b.Width > 0 {
		// Thumbnails are made at 2×.
		scale = min(thumbMaxWidth/w, thumbMaxHeight/h, 0.5)
	}
	return max(w*scale, 8), max(h*scale, 8)
}

func countText(n int) string { return fmt.Sprintf("共 %d 条", n) }

// RelativeTime is "刚刚", "N 分钟前", "N 小时前", "昨天" or "M月D日".
func RelativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d/time.Minute))
	}
	ny, nm, nd := now.Date()
	ty, tm, td := t.Date()
	if ny == ty && nm == tm && nd == td {
		return fmt.Sprintf("%d 小时前", int(d/time.Hour))
	}
	yesterday := now.AddDate(0, 0, -1)
	if yy, ym, yd := yesterday.Date(); yy == ty && ym == tm && yd == td {
		return "昨天"
	}
	return fmt.Sprintf("%d月%d日", int(tm), td)
}
