package longshot

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

var (
	barFeature = sui.Tree.Feature("longshot-bar")
	barFinish  = sui.Tree.ID(barFeature, "finish")
	barCancel  = sui.Tree.ID(barFeature, "cancel")
	barAuto    = sui.Tree.ID(barFeature, "enable-auto")
)

// BarTitle is the control bar window's unique title.
const BarTitle = "snaplite-longshot-bar"

// Bar sizes (docs/02 §3.2).
const (
	BarWidth     = 360
	BarWideWidth = 480 // with the "开启自动滚动" link
	BarHeight    = 44
)

// Bar is the long-shot control bar: status, 取消 and 完成.
type Bar struct {
	progress Progress
	tokens   sui.Tokens
	view     flex.View
	wake     app.Invalidator
	// AskAuto shows the "开启自动滚动" link (manual because of a missing permission).
	AskAuto bool

	OnFinish, OnCancel, OnEnableAuto func()
}

// NewBar makes the bar with its first status text.
func NewBar(tokens sui.Tokens, message string, askAuto bool) *Bar {
	return &Bar{tokens: tokens, progress: Progress{Message: message}, AskAuto: askAuto}
}

// Width is the bar's width.
func (b *Bar) Width() float64 {
	if b.AskAuto {
		return BarWideWidth
	}
	return BarWidth
}

// Update shows new progress; it runs on the event-loop thread.
func (b *Bar) Update(p Progress) {
	if p.Message == "" {
		p.Message = b.progress.Message
	}
	b.progress = p
	b.wake.Invalidate()
}

// Message is the status text shown.
func (b *Bar) Message() string { return b.progress.Message }

// Config is the bar's frameless window at origin (screen logical coordinates).
func (b *Bar) Config(origin vui.Point) app.Config {
	return app.Config{
		Title: BarTitle, WindowMode: app.WindowFrameless, Width: int(b.Width()), Height: BarHeight,
		Origin: &origin, Tree: &sui.Tree, Paint: b.paint, Invalidator: &b.wake,
		OnKey: func(e vui.KeyEvent) vui.EventResult {
			if e.Key == vui.KeyEscape && b.OnCancel != nil {
				b.OnCancel()
				return vui.EventResult{Handled: true}
			}
			return vui.EventResult{}
		},
	}
}

func call(fn func()) func() {
	return func() {
		if fn != nil {
			fn()
		}
	}
}

func (b *Bar) paint(canvas *vui.Canvas, size vui.Size) {
	t := b.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	canvas.RoundedBorder(bounds, sui.RadiusSurface, 1, t.SurfaceBorder, t.Surface)
	b.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
		f.Row().Fill().PadX(12).Gap(8).Items(flex.Center).Children(func() {
			f.Text(b.progress.Message).FontSize(sui.ControlTextSz).Ellipsis().Grow(1).Shrink(1)
			if b.AskAuto {
				if f.Button(barAuto, "开启自动滚动").Link().Small().Clicked() {
					canvas.AfterPaint(call(b.OnEnableAuto))
				}
			}
			if f.Button(barCancel, "取消").Ghost().Small().Clicked() {
				canvas.AfterPaint(call(b.OnCancel))
			}
			if f.Button(barFinish, "完成").Small().Clicked() {
				canvas.AfterPaint(call(b.OnFinish))
			}
		})
	})
}

// PlaceBar puts a bar of size w×h 8 px below the selection, centred, or
// above it, never over it (it would be captured). When neither fits, it goes
// to the display's bottom-right corner.
func PlaceBar(sel, display platform.Rect, w, h float64) platform.Rect {
	const gap = sui.FloatGap
	x := sel.X + (sel.W-w)/2
	x = min(max(x, display.X+gap), display.X+display.W-w-gap)
	if y := sel.Y + sel.H + gap; y+h <= display.Y+display.H {
		return platform.Rect{X: x, Y: y, W: w, H: h}
	}
	if y := sel.Y - gap - h; y >= display.Y {
		return platform.Rect{X: x, Y: y, W: w, H: h}
	}
	return platform.Rect{X: display.X + display.W - w - gap, Y: display.Y + display.H - h - gap, W: w, H: h}
}
