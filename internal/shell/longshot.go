package shell

import (
	"image"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/editor"
	"snaplite/internal/longshot"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

// longShotRun is a long shot in progress.
type longShotRun struct {
	session  *longshot.Session
	bar      *longshot.Bar
	window   *app.Window
	scroller *nativeScroller
	display  platform.Display
}

type nativeCapturer struct {
	display uint32
	rect    platform.Rect
}

func (c nativeCapturer) Capture() (*image.RGBA, error) {
	return platform.CaptureRect(c.display, c.rect)
}

// nativeScroller moves the pointer to the region's centre before the first
// scroll (applications scroll the view under the pointer) and puts it back
// at the end.
type nativeScroller struct {
	x, y         float64
	moved        bool
	backX, backY float64
}

func (s *nativeScroller) Scroll(dy float64) error {
	if !s.moved {
		s.backX, s.backY = platform.CursorPosition()
		if err := platform.MoveCursor(s.x, s.y); err != nil {
			return err
		}
		s.moved = true
	}
	return platform.ScrollAt(s.x, s.y, dy)
}

func (s *nativeScroller) restore() {
	if s.moved {
		_ = platform.MoveCursor(s.backX, s.backY)
	}
}

// startLongShot runs a long shot of rect (screen logical coordinates) on display.
func (s *Shell) startLongShot(display platform.Display, rect platform.Rect) {
	defer s.guard("longshot")
	mode := longshot.Auto
	askAuto := false
	if s.cfg.LongShot.Mode == "manual" {
		mode = longshot.Manual
	} else if !platform.HasAccessibilityPermission() {
		// Wheel events need the Accessibility permission on macOS; without it
		// the user scrolls and the bar offers to turn automatic scrolling on.
		platform.RequestAccessibilityPermission()
		mode, askAuto = longshot.Manual, true
	}
	message := longshot.RunningMessage(1, 0)
	if mode == longshot.Manual {
		message = longshot.ManualMessage
	}
	bar := longshot.NewBar(sui.Current(), message, askAuto)
	place := longshot.PlaceBar(rect, display.Bounds, bar.Width(), longshot.BarHeight)
	w, err := s.app.OpenWindow(bar.Config(vui.Point{X: float32(place.X), Y: float32(place.Y)}), new(vui.Canvas))
	if err != nil {
		s.log.Printf("long-shot bar: %v", err)
		s.busy = false
		return
	}
	s.place(longshot.BarTitle, place, platform.FloatWindow)
	s.place(longshot.BarTitle, place, func(title string, _ platform.Rect) error { return platform.SetExcludedFromCapture(title) })
	cx, cy := rect.Center()
	run := &longShotRun{bar: bar, window: w, display: display, scroller: &nativeScroller{x: cx, y: cy}}
	scale := display.Scale
	if scale <= 0 {
		scale = 1
	}
	run.session = longshot.Start(longshot.Options{
		Mode: mode, Height: rect.H, Scale: scale, MaxHeight: s.cfg.LongShot.MaxHeight,
		Delay:    150 * time.Millisecond,
		Capturer: nativeCapturer{display: display.ID, rect: rect},
		Scroller: run.scroller,
		Post:     platform.PostMain,
	}, func(p longshot.Progress) { s.longShotProgress(run, p) })
	bar.OnFinish = run.session.Finish
	bar.OnCancel = run.session.Cancel
	bar.OnEnableAuto = func() { platform.OpenPrivacySettings("accessibility") }
	s.extra.long = run
}

func (s *Shell) longShotProgress(run *longShotRun, p longshot.Progress) {
	defer s.guard("longshot progress")
	run.bar.Update(p)
	if p.State == longshot.Running || p.State == longshot.Paused {
		return
	}
	run.window.Close()
	run.scroller.restore()
	if s.extra.long == run {
		s.extra.long = nil
	}
	s.busy = false
	img := run.session.Result()
	if p.State == longshot.Canceled || img == nil {
		return
	}
	s.openEditor(img, run.display)
}

func (s *Shell) openEditor(img *image.RGBA, display platform.Display) {
	e := editor.New(img, display.Scale, sui.Current())
	w, h := e.InitialSize(display.Bounds.W, display.Bounds.H)
	config := e.Config(w, h)
	config.Origin = &vui.Point{
		X: float32(display.Bounds.X + (display.Bounds.W-float64(w))/2),
		Y: float32(display.Bounds.Y + (display.Bounds.H-float64(h))/2),
	}
	e.OnCopy = func(img *image.RGBA, shapes []annotate.Shape) {
		go func() {
			out := raster.Compose(img, shapes)
			data, err := raster.EncodePNG(out)
			platform.PostMain(func() {
				if err != nil {
					s.log.Printf("encode: %v", err)
					return
				}
				s.deliver(out, data)
			})
		}()
	}
	e.OnSave = func(img *image.RGBA, shapes []annotate.Shape) bool {
		path, ok := s.askSavePath()
		if !ok {
			return false
		}
		go func() {
			out := raster.Compose(img, shapes)
			data, err := raster.EncodePNG(out)
			if err == nil {
				err = writeFile(path, data)
			}
			platform.PostMain(func() {
				if err != nil {
					s.log.Printf("save: %v", err)
					return
				}
				s.deliver(out, data)
			})
		}()
		return true
	}
	if _, err := s.app.OpenWindow(config, new(vui.Canvas)); err != nil {
		s.log.Printf("editor: %v", err)
		return
	}
	platform.ActivateApp()
}
