package editor

import (
	"fmt"
	"image"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

var (
	feature       = sui.Tree.Feature("editor")
	canvasID      = sui.Tree.ID(feature, "image")
	confirmScope  = sui.Tree.Sub(feature, sui.Tree.Name("confirm"))
	confirmFocus  = sui.Tree.ID(confirmScope, "scope")
	confirmKeep   = sui.Tree.ID(confirmScope, "keep")
	confirmLeave  = sui.Tree.ID(confirmScope, "discard")
	toolbarMargin = float32(8)
)

// Layout (docs/02 §3.3 and docs/05 §7).
const (
	// TileRows is the height of one texture tile: long images exceed GPU
	// texture limits (often 8192 or 16384), so they are drawn in strips.
	TileRows     = 2048
	TitlebarH    = 44
	sideMargin   = 24
	topPadding   = 16
	DiscardTitle = "放弃修改？"
)

type tile struct {
	bitmap vui.Bitmap
	y0     int // first image row
}

// Editor shows a long image, scrolls it and lets the user annotate it.
type Editor struct {
	img    *image.RGBA
	scale  float64 // image pixels per logical pixel at 100 %
	tiles  []tile
	tokens sui.Tokens
	Doc    annotate.Document
	title  string

	size    vui.Size
	scrollY float32
	drawing bool
	panning bool
	panFrom vui.Point
	panY    float32

	confirm    bool
	allowClose func()
	view       flex.View
	wake       app.Invalidator

	// OnCopy composes and copies; OnSave asks where to save and reports
	// whether the user chose a place. Both get the shapes in image pixels.
	OnCopy func(img *image.RGBA, shapes []annotate.Shape)
	OnSave func(img *image.RGBA, shapes []annotate.Shape) bool
}

// New prepares the image's tiles. scale is the display scale it was captured at.
func New(img *image.RGBA, scale float64, tokens sui.Tokens) *Editor {
	if scale <= 0 {
		scale = 1
	}
	e := &Editor{img: img, scale: scale, tokens: tokens, Doc: annotate.NewDocument()}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := raster.ToBGRA(nil, img)
	for y0 := 0; y0 < h; y0 += TileRows {
		rows := min(TileRows, h-y0)
		e.tiles = append(e.tiles, tile{y0: y0, bitmap: vui.Bitmap{
			Pix: pix[y0*w*4 : (y0+rows)*w*4], Width: w, Height: rows, Stride: w * 4,
			ID: raster.NewBitmapID(), Version: 1,
		}})
	}
	e.title = fmt.Sprintf("长截图 · %d × %d", w, h)
	return e
}

// Title is the window title.
func (e *Editor) Title() string { return e.title }

// TileIDs are the bitmap identities of the tiles, top first.
func (e *Editor) TileIDs() []uint64 {
	out := make([]uint64, len(e.tiles))
	for i, t := range e.tiles {
		out[i] = t.bitmap.ID
	}
	return out
}

// InitialSize is the window size for a screen of the given logical size:
// the image at 100 % plus margins, at most 80 % of the screen.
func (e *Editor) InitialSize(screenW, screenH float64) (int, int) {
	w := float64(e.img.Bounds().Dx())/e.scale + 2*sideMargin
	return int(min(w, screenW*0.8)), int(screenH * 0.8)
}

// Zoom is window logical pixels per image pixel: 100 % (1/scale) or less,
// so the image fits the window's width. It only shrinks.
func (e *Editor) Zoom() float32 {
	natural := float32(1 / e.scale)
	fit := (e.size.Width - 2*sideMargin) / float32(e.img.Bounds().Dx())
	return max(min(natural, fit), 0.01)
}

func (e *Editor) viewport() float32 { return max(e.size.Height-TitlebarH, 0) }

// MaxScroll is the furthest the content scrolls.
func (e *Editor) MaxScroll() float32 {
	content := float32(e.img.Bounds().Dy())*e.Zoom() + 2*topPadding
	return max(content-e.viewport(), 0)
}

// ScrollY is the scroll offset in logical pixels.
func (e *Editor) ScrollY() float32 { return e.scrollY }

func (e *Editor) setScroll(y float32) { e.scrollY = min(max(y, 0), e.MaxScroll()) }

func (e *Editor) annotateView() annotate.View {
	z := e.Zoom()
	return annotate.View{Origin: vui.Point{
		X: (e.size.Width - float32(e.img.Bounds().Dx())*z) / 2,
		Y: TitlebarH + topPadding - e.scrollY,
	}, Zoom: z}
}

func (e *Editor) imageRect() vui.Rect {
	v := e.annotateView()
	b := e.img.Bounds()
	return vui.Rect{X: v.Origin.X, Y: v.Origin.Y, Width: float32(b.Dx()) * v.Zoom, Height: float32(b.Dy()) * v.Zoom}
}

// VisibleTiles is the range [first, last) of tiles that meet the viewport.
func (e *Editor) VisibleTiles() (first, last int) {
	v := e.annotateView()
	top, bottom := float32(TitlebarH), e.size.Height
	first, last = -1, -1
	for i, t := range e.tiles {
		y0 := v.Origin.Y + float32(t.y0)*v.Zoom
		y1 := y0 + float32(t.bitmap.Height)*v.Zoom
		if y1 > top && y0 < bottom {
			if first < 0 {
				first = i
			}
			last = i + 1
		}
	}
	if first < 0 {
		return 0, 0
	}
	return first, last
}

// Config is the editor window.
func (e *Editor) Config(width, height int) app.Config {
	e.size = vui.Size{Width: float32(width), Height: float32(height)}
	return app.Config{
		Title: e.title, WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarH,
		Width: width, Height: height, MinWidth: 480, MinHeight: 320, Tree: &sui.Tree,
		Paint:          e.paint,
		OnPointer:      e.pointer,
		OnScroll:       e.scroll,
		OnKey:          e.key,
		OnShortcut:     e.shortcut,
		OnResize:       e.resize,
		OnCloseRequest: e.closeRequest,
		Invalidator:    &e.wake,
	}
}

func (e *Editor) resize(size vui.Size) {
	e.size = size
	e.setScroll(e.scrollY)
}

func (e *Editor) closeRequest(allow func()) {
	if !e.Doc.Dirty() {
		allow()
		return
	}
	e.confirm, e.allowClose = true, allow
	e.wake.Invalidate()
}

// Confirming reports whether the discard dialog is open.
func (e *Editor) Confirming() bool { return e.confirm }

func (e *Editor) scroll(ev vui.ScrollEvent) vui.EventResult {
	e.setScroll(e.scrollY + ev.DeltaY)
	return vui.EventResult{Handled: true, Repaint: true}
}

func (e *Editor) toImage(p vui.Point) image.Point {
	ip := e.annotateView().ToImage(p)
	b := e.img.Bounds()
	ip.X = min(max(ip.X, 0), b.Dx())
	ip.Y = min(max(ip.Y, 0), b.Dy())
	return ip
}

var handled = vui.EventResult{Handled: true, Repaint: true}

func (e *Editor) pointer(ev vui.PointerEvent) vui.EventResult {
	if e.confirm || sui.IsToolbar(ev.Target) {
		return vui.EventResult{}
	}
	p := vui.Point{X: ev.X, Y: ev.Y}
	switch ev.Kind {
	case vui.PointerDown:
		if ev.Button != vui.PointerButtonPrimary || p.Y < TitlebarH {
			return vui.EventResult{}
		}
		if e.Doc.Tool != annotate.ToolNone && contains(e.imageRect(), p) {
			e.Doc.Begin(e.toImage(p))
			e.drawing = true
			return handled
		}
		e.panning, e.panFrom, e.panY = true, p, e.scrollY
		return handled
	case vui.PointerMove:
		switch {
		case e.drawing:
			e.Doc.Extend(e.toImage(p), ev.Modifiers&vui.ModifierShift != 0)
			return handled
		case e.panning:
			e.setScroll(e.panY - (p.Y - e.panFrom.Y))
			return handled
		}
	case vui.PointerUp:
		if e.drawing {
			e.Doc.Extend(e.toImage(p), ev.Modifiers&vui.ModifierShift != 0)
			e.Doc.Commit()
			e.drawing = false
			return handled
		}
		if e.panning {
			e.panning = false
			return handled
		}
	}
	return vui.EventResult{}
}

func contains(r vui.Rect, p vui.Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.Width && p.Y < r.Y+r.Height
}

func (e *Editor) key(ev vui.KeyEvent) vui.EventResult {
	if e.confirm {
		return vui.EventResult{}
	}
	switch ev.Key {
	case vui.KeyEscape:
		if e.drawing {
			e.Doc.CancelDraft()
			e.drawing = false
		} else {
			e.Doc.Tool = annotate.ToolNone
		}
		return handled
	case vui.KeyPageDown, vui.KeySpace:
		e.setScroll(e.scrollY + e.viewport()*0.9)
		return handled
	case vui.KeyPageUp:
		e.setScroll(e.scrollY - e.viewport()*0.9)
		return handled
	case vui.KeyHome:
		e.setScroll(0)
		return handled
	case vui.KeyEnd:
		e.setScroll(e.MaxScroll())
		return handled
	case vui.KeyDown:
		e.setScroll(e.scrollY + 40)
		return handled
	case vui.KeyUp:
		e.setScroll(e.scrollY - 40)
		return handled
	}
	return vui.EventResult{}
}

func (e *Editor) shortcut(ev vui.KeyEvent) vui.EventResult {
	if e.confirm || ev.Modifiers&(vui.ModifierMeta|vui.ModifierControl) == 0 {
		return vui.EventResult{}
	}
	shift := ev.Modifiers&vui.ModifierShift != 0
	switch strings.ToLower(ev.Text) {
	case "z":
		if shift {
			e.Doc.Redo()
		} else {
			e.Doc.Undo()
		}
	case "y":
		e.Doc.Redo()
	case "c":
		e.act(sui.ActCopy)
	case "s":
		e.act(sui.ActSave)
	case "1", "2", "3", "4":
		e.toggleTool(annotate.Tool(ev.Text[0]-'1') + annotate.ToolRect)
	default:
		return vui.EventResult{}
	}
	return handled
}

func (e *Editor) toggleTool(t annotate.Tool) {
	if e.Doc.Tool == t {
		e.Doc.Tool = annotate.ToolNone
	} else {
		e.Doc.Tool = t
	}
}

func (e *Editor) act(a sui.Action) {
	switch a {
	case sui.ActRect, sui.ActEllipse, sui.ActArrow, sui.ActPen:
		e.toggleTool(sui.ActionTool(a))
	case sui.ActUndo:
		e.Doc.Undo()
	case sui.ActRedo:
		e.Doc.Redo()
	case sui.ActCopy:
		if e.OnCopy != nil {
			e.OnCopy(e.img, e.Doc.Snapshot())
			e.Doc.MarkClean()
		}
	case sui.ActSave:
		if e.OnSave != nil && e.OnSave(e.img, e.Doc.Snapshot()) {
			e.Doc.MarkClean()
		}
	}
}

func (e *Editor) paint(canvas *vui.Canvas, size vui.Size) {
	t := e.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, t.Workspace)

	content := vui.Rect{Y: TitlebarH, Width: size.Width, Height: max(size.Height-TitlebarH, 0)}
	cursor := vui.PointerCursorDefault
	if e.Doc.Tool != annotate.ToolNone {
		cursor = vui.PointerCursorCrosshair
	}
	canvas.HitWithOptions(canvasID, content, "长截图", vui.HitOptions{Cursor: cursor, SkipFocus: true})
	canvas.Clip(content)
	v := e.annotateView()
	first, last := e.VisibleTiles()
	for _, tl := range e.tiles[first:last] {
		canvas.DrawBitmap(tl.bitmap, vui.Rect{
			X: v.Origin.X, Y: v.Origin.Y + float32(tl.y0)*v.Zoom,
			Width: float32(tl.bitmap.Width) * v.Zoom, Height: float32(tl.bitmap.Height) * v.Zoom,
		})
	}
	canvas.Unclip()
	// Viva paints a layer's images after its rectangles and paths, so the
	// annotations and the title bar go one layer above the tiles.
	canvas.PushLayer(1)
	defer canvas.PopLayer()
	img := e.imageRect()
	clip := vui.Rect{X: img.X, Y: max(img.Y, TitlebarH), Width: img.Width}
	clip.Height = max(min(img.Y+img.Height, size.Height)-clip.Y, 0)
	annotate.Draw(canvas, &e.Doc, clip, v)

	// Title bar: title on the leading side, the toolbar on the trailing side.
	bar := vui.Rect{Width: size.Width, Height: TitlebarH}
	canvas.Fill(bar, t.Surface)
	canvas.Fill(vui.Rect{Y: TitlebarH - 1, Width: size.Width, Height: 1}, t.SurfaceBorder)
	leading, trailing := app.TitlebarInsets()
	canvas.MoveRegion(bar)
	canvas.Label(e.title, vui.Rect{X: leading + 12, Width: size.Width / 2, Height: TitlebarH},
		vui.TextStyle{Size: sui.BodyTextSize, Weight: vui.WeightMedium, Color: t.Text})
	spec := sui.ToolbarSpec{Tools: true, Copy: true, Save: true, Tool: e.Doc.Tool, CanUndo: e.Doc.CanUndo(), CanRedo: e.Doc.CanRedo()}
	tb := sui.ToolbarSize(canvas, spec)
	origin := vui.Point{X: size.Width - trailing - tb.Width - toolbarMargin, Y: (TitlebarH - tb.Height) / 2}
	r := sui.DrawToolbar(canvas, t, spec, origin)
	if a := sui.ToolbarClick(canvas); a != sui.ActNone {
		canvas.AfterPaint(func() { e.act(a) })
	}
	if e.Doc.Tool != annotate.ToolNone {
		props := sui.PropsSize()
		sui.DrawProps(canvas, t, e.Doc.Color, e.Doc.Width, vui.Point{X: r.X + r.Width - props.Width, Y: r.Y + r.Height + sui.PropsGap})
		if ci, wi := sui.PropsClick(canvas); ci >= 0 || wi >= 0 {
			canvas.AfterPaint(func() {
				if ci >= 0 {
					e.Doc.Color = annotate.Palette[ci]
				}
				if wi >= 0 {
					e.Doc.Width = annotate.Widths[wi]
				}
			})
		}
	}

	if e.confirm {
		e.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
			d := f.Dialog(flex.DialogSpec{
				Title: DiscardTitle, Description: "标注还没有复制或保存，关闭后会丢失。", Width: 360,
				Scope: confirmFocus, DismissID: confirmKeep, NoClose: true,
			})
			d.Footer(func() {
				f.Button(confirmKeep, "继续编辑").Outline()
				if f.Button(confirmLeave, "放弃").Destructive().Clicked() {
					canvas.AfterPaint(func() {
						e.confirm = false
						if e.allowClose != nil {
							e.allowClose()
						}
					})
				}
			})
			if d.Dismissed() {
				canvas.AfterPaint(func() { e.confirm = false })
			}
		})
	}
}
