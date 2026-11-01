package pin

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/i18n"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

var (
	feature     = sui.Tree.Feature("pin")
	surfaceID   = sui.Tree.ID(feature, "surface")
	menuScope   = sui.Tree.ID(feature, "menu")
	menuDismiss = sui.Tree.ID(feature, "menu-dismiss")
	menuCopy    = sui.Tree.ID(feature, "copy")
	menuSave    = sui.Tree.ID(feature, "save")
	menuUnpin   = sui.Tree.ID(feature, "unpin")
)

// Geometry (logical pixels).
const (
	// Cascade is how far each further pin opened at one place is offset.
	Cascade = 24
	// ScreenShare is the largest part of its screen an image pin covers;
	// TextScreenShare the tallest a text card gets.
	ScreenShare     = 0.8
	TextScreenShare = 0.6
	TextPadding     = 12
	TextMaxWidth    = 360
	textLine        = 20

	menuWidth  = 160
	menuHeight = 3*32 + 9 + 2*4 // three rows, a separator, the surface's padding
	menuLayer  = 2
)

// Title is the unique window title of the n-th pin, which the platform layer
// finds it by.
func Title(n int) string { return fmt.Sprintf("snaplite-pin-%d", n) }

// Pin is one pinned picture or text.
type Pin struct {
	title  string
	tokens sui.Tokens
	image  *image.RGBA // nil for a text
	text   string
	bitmap vui.Bitmap
	lines  []string // the text's lines as the card shows them
	// frame is where the pin is, in screen logical coordinates; grown is
	// true while the window is enlarged to hold the context menu.
	frame platform.Rect
	grown bool

	dragging       bool
	pressX, pressY float32 // the press in screen coordinates
	pressFrame     platform.Rect

	menu   components.ContextMenuState
	menuAt vui.Point

	// Move places the window at a frame in screen logical coordinates
	// (platform.FloatWindow); tests record it instead.
	Move func(frame platform.Rect)
	// OnClose closes the window (a double click, Escape, 取消钉住).
	OnClose func()
	// OnCopy and OnSave are the context menu's 复制 and 保存到本地….
	OnCopy func()
	OnSave func()
}

// NewImage pins a picture in frame; the window is frame's size and the
// picture is scaled into it.
func NewImage(title string, img *image.RGBA, frame platform.Rect, tokens sui.Tokens) *Pin {
	return &Pin{title: title, tokens: tokens, image: img, bitmap: raster.Bitmap(img), frame: roundFrame(frame)}
}

// NewText pins a text card in frame showing lines (TextLayout's).
func NewText(title, text string, lines []string, frame platform.Rect, tokens sui.Tokens) *Pin {
	return &Pin{title: title, tokens: tokens, text: text, lines: lines, frame: roundFrame(frame)}
}

// roundFrame gives the window whole logical pixels, as Config's Width and
// Height are integers.
func roundFrame(r platform.Rect) platform.Rect {
	return platform.Rect{X: math.Round(r.X), Y: math.Round(r.Y), W: max(math.Round(r.W), 1), H: max(math.Round(r.H), 1)}
}

// Image is the pinned picture, nil for a text.
func (p *Pin) Image() *image.RGBA { return p.image }

// Text is the pinned text, "" for a picture.
func (p *Pin) Text() string { return p.text }

// Title is the window's title.
func (p *Pin) Title() string { return p.title }

// Frame is where the pin is, without the room a context menu takes.
func (p *Pin) Frame() platform.Rect { return p.frame }

// Config is the pin's window: frameless, at its frame.
func (p *Pin) Config() app.Config {
	return app.Config{
		Title: p.title, WindowMode: app.WindowFrameless,
		Width: int(p.frame.W), Height: int(p.frame.H),
		Origin:    &vui.Point{X: float32(p.frame.X), Y: float32(p.frame.Y)},
		Tree:      &sui.Tree,
		Paint:     p.paint,
		OnPointer: p.pointer,
		OnKey:     p.key,
	}
}

// ImageSize is the window size of a picture shown at its natural size on a
// display of scale (physical pixels per logical pixel), shrunk to fit in
// ScreenShare of screen.
func ImageSize(img *image.RGBA, scale float64, screen platform.Rect) (w, h float64) {
	if scale <= 0 {
		scale = 1
	}
	w, h = float64(img.Bounds().Dx())/scale, float64(img.Bounds().Dy())/scale
	fit := min(1, screen.W*ScreenShare/w, screen.H*ScreenShare/h)
	return max(w*fit, 1), max(h*fit, 1)
}

// TextLayout wraps text for a card: 13px lines no wider than TextMaxWidth less
// the padding, no taller than TextScreenShare of screen, the last line ending
// in an ellipsis when the text does not fit. It returns the lines and the
// card's size. measure is a canvas measuring as the windows draw (Measure).
func TextLayout(measure *vui.Canvas, text string, screen platform.Rect) (lines []string, w, h float64) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", "    ")
	text = strings.TrimRight(text, "\n ")
	maxLines := max(int((screen.H*TextScreenShare-2*TextPadding)/textLine), 1)
	width := float32(TextMaxWidth - 2*TextPadding)
	lines = measure.WrapLines(nil, text, sui.BodyTextSize, vui.WeightRegular, width, maxLines)
	if len(lines) == 0 {
		lines = []string{""}
	}
	widest := float32(0)
	for _, line := range lines {
		widest = max(widest, measure.TextWidthWeight(line, sui.BodyTextSize, vui.WeightRegular))
	}
	w = float64(min(widest, width)) + 2*TextPadding
	h = float64(len(lines)*textLine) + 2*TextPadding
	return lines, math.Ceil(w), h
}

// Measure is a canvas that measures text with the platform's own engine, as
// the windows draw it.
func Measure() *vui.Canvas {
	canvas := new(vui.Canvas)
	if engine := app.PlatformTextEngine(); engine != nil {
		canvas.SetTextEngine(engine)
	}
	return canvas
}

// Centered places a w×h pin in the middle of screen, offset by n×Cascade down
// and to the right; the offset starts over where it would leave the screen.
func Centered(screen platform.Rect, w, h float64, n int) platform.Rect {
	x, y := screen.X+(screen.W-w)/2, screen.Y+(screen.H-h)/2
	room := min(screen.X+screen.W-w-x, screen.Y+screen.H-h-y)
	steps := 1
	if room > 0 {
		steps = int(room/Cascade) + 1
	}
	offset := float64(n%steps) * Cascade
	return platform.Rect{X: x + offset, Y: y + offset, W: w, H: h}
}

// Within places a w×h pin centred on sel: exactly over it when the sizes match.
func Within(sel platform.Rect, w, h float64) platform.Rect {
	return platform.Rect{X: sel.X + (sel.W-w)/2, Y: sel.Y + (sel.H-h)/2, W: w, H: h}
}

func (p *Pin) close() {
	if p.OnClose != nil {
		p.OnClose()
	}
}

// place moves the window to the frame, enlarged while the menu needs room.
func (p *Pin) place() {
	if p.Move == nil {
		return
	}
	f := p.frame
	if p.grown {
		f.W, f.H = max(f.W, menuWidth+8), max(f.H, menuHeight+8)
	}
	p.Move(f)
}

var handled = vui.EventResult{Handled: true, Repaint: true}

// pointer drags the pin, closes it on a double click and opens the menu on a
// secondary press. Presses on the open menu and its backdrop are the menu's.
func (p *Pin) pointer(e vui.PointerEvent) vui.EventResult {
	switch e.Kind {
	case vui.PointerDown:
		if e.Target != surfaceID {
			return vui.EventResult{}
		}
		switch e.Button {
		case vui.PointerButtonPrimary:
			if e.ClickCount >= 2 {
				p.dragging = false
				p.close()
				return handled
			}
			p.dragging = true
			p.pressX, p.pressY, p.pressFrame = e.ScreenX, e.ScreenY, p.frame
			return vui.EventResult{Handled: true}
		case vui.PointerButtonSecondary:
			p.openMenu(vui.Point{X: e.X, Y: e.Y})
			return handled
		}
	case vui.PointerMove:
		if !p.dragging {
			return vui.EventResult{}
		}
		// Screen coordinates: the window moves under the pointer, so its own
		// coordinates would not say how far the pointer went.
		f := p.pressFrame
		f.X += float64(e.ScreenX - p.pressX)
		f.Y += float64(e.ScreenY - p.pressY)
		if f != p.frame {
			p.frame = f
			p.place()
		}
		return vui.EventResult{Handled: true}
	case vui.PointerUp:
		if p.dragging && e.Button == vui.PointerButtonPrimary {
			p.dragging = false
			return vui.EventResult{Handled: true}
		}
	}
	return vui.EventResult{}
}

// openMenu shows the context menu at a window point, enlarging a pin too small
// to hold it for as long as it is open.
func (p *Pin) openMenu(at vui.Point) {
	p.menu, p.menuAt = p.menu.Open(), at
	if !p.grown && (p.frame.W < menuWidth+8 || p.frame.H < menuHeight+8) {
		p.grown = true
		p.place()
	}
}

func (p *Pin) closeMenu() {
	p.menu = p.menu.Close()
	if p.grown {
		p.grown = false
		p.place()
	}
}

// key closes the pin on Escape; with the menu open, Escape is the menu's.
func (p *Pin) key(e vui.KeyEvent) vui.EventResult {
	if e.Key != vui.KeyEscape || p.menu.IsOpen() {
		return vui.EventResult{}
	}
	p.close()
	return vui.EventResult{Handled: true}
}

// paint draws the picture or the card, the border and the menu. Viva paints a
// layer's rectangles before its images and texts, so the border and the menu
// are on higher layers than the picture, or it would cover them.
func (p *Pin) paint(canvas *vui.Canvas, size vui.Size) {
	t := p.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	content := vui.Rect{Width: min(float32(p.frame.W), size.Width), Height: min(float32(p.frame.H), size.Height)}
	label := i18n.T("钉住的文字", "Pinned text")
	if p.image != nil {
		label = i18n.T("钉住的图片", "Pinned image")
	}
	canvas.HitWithOptions(surfaceID, content, label, vui.HitOptions{SkipFocus: true})
	if p.image == nil || content != bounds {
		// The card, and the room an enlarged window has around a picture.
		canvas.Fill(bounds, t.Surface)
	}
	if p.image != nil {
		canvas.DrawBitmap(p.bitmap, content)
	} else {
		style := vui.TextStyle{Size: sui.BodyTextSize, Color: t.Text}
		for i, line := range p.lines {
			canvas.Label(line, vui.Rect{X: TextPadding, Y: TextPadding + float32(i*textLine), Width: content.Width - 2*TextPadding, Height: textLine}, style)
		}
	}
	canvas.PushLayer(1)
	strokeInside(canvas, content, 1, t.SurfaceBorder)
	canvas.PopLayer()
	if p.menu.IsOpen() {
		p.paintMenu(canvas, bounds)
	}
}

// strokeInside draws a w-wide frame along the inside of r.
func strokeInside(canvas *vui.Canvas, r vui.Rect, w float32, color vui.Color) {
	canvas.Fill(vui.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: w}, color)
	canvas.Fill(vui.Rect{X: r.X, Y: r.Y + r.Height - w, Width: r.Width, Height: w}, color)
	canvas.Fill(vui.Rect{X: r.X, Y: r.Y + w, Width: w, Height: r.Height - 2*w}, color)
	canvas.Fill(vui.Rect{X: r.X + r.Width - w, Y: r.Y + w, Width: w, Height: r.Height - 2*w}, color)
}

func (p *Pin) paintMenu(canvas *vui.Canvas, bounds vui.Rect) {
	at := vui.Rect{X: p.menuAt.X, Y: p.menuAt.Y, Width: menuWidth, Height: menuHeight}
	menu := components.BeginContextMenu(canvas, p.tokens.VivaTheme(), at, components.ContextMenuOptions{
		Layer: menuLayer, Scope: menuScope, DismissID: menuDismiss, Viewport: bounds,
	})
	menu.AddItem(components.ContextMenuItem{ID: menuCopy, Label: i18n.T("复制", "Copy")})
	menu.AddItem(components.ContextMenuItem{ID: menuSave, Label: i18n.T("保存到本地…", "Save As…")})
	menu.AddSeparator()
	menu.AddItem(components.ContextMenuItem{ID: menuUnpin, Label: i18n.T("取消钉住", "Unpin")})
	menu.End()
	then := func(fn func()) {
		canvas.AfterPaint(func() {
			p.closeMenu()
			if fn != nil {
				fn()
			}
		})
	}
	switch {
	case canvas.Clicked(menuCopy):
		then(p.OnCopy)
	case canvas.Clicked(menuSave):
		then(p.OnSave)
	case canvas.Clicked(menuUnpin):
		then(p.close)
	case canvas.Clicked(menuDismiss):
		then(nil)
	}
}
