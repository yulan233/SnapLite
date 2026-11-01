package shell

import (
	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/history"
	"snaplite/internal/pin"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

// pinWindow is an open pin.
type pinWindow struct {
	pin    *pin.Pin
	window *app.Window
}

// nextPinTitle is a title no other pin of this run has.
func (s *Shell) nextPinTitle() string {
	s.pinCount++
	return pin.Title(s.pinCount)
}

// openPin opens a pin's window and floats it above other applications'
// windows without taking the keyboard.
func (s *Shell) openPin(p *pin.Pin) {
	defer s.guard("pin")
	title := p.Title()
	p.Move = func(frame platform.Rect) {
		if err := platform.FloatWindow(title, frame); err != nil {
			s.log.Printf("pin %s: %v", title, err)
		}
	}
	entry := &pinWindow{pin: p}
	config := p.Config()
	config.OnClosed = func() { s.forgetPin(entry) }
	w, err := s.app.OpenWindow(config, new(vui.Canvas))
	if err != nil {
		s.log.Printf("pin window: %v", err)
		return
	}
	entry.window = w
	p.OnClose = w.Close
	p.OnCopy = func() { s.copyPin(p) }
	p.OnSave = func() { s.savePin(p) }
	s.pins = append(s.pins, entry)
	s.place(title, p.Frame(), platform.FloatWindow)
}

func (s *Shell) forgetPin(entry *pinWindow) {
	for i, e := range s.pins {
		if e == entry {
			s.pins = append(s.pins[:i], s.pins[i+1:]...)
			return
		}
	}
}

// CloseAllPins closes every pin (the tray's 关闭全部钉图, and quitting).
func (s *Shell) CloseAllPins() {
	for _, e := range append([]*pinWindow(nil), s.pins...) {
		e.window.Close()
	}
	s.pins = nil
}

// copyPin puts a pin's picture (as PNG, encoded in the background) or text
// on the clipboard. Like a restore from the history, it is not recorded again.
func (s *Shell) copyPin(p *pin.Pin) {
	if img := p.Image(); img != nil {
		go func() {
			data, err := raster.EncodePNG(img)
			platform.PostMain(func() {
				if err == nil {
					err = systemClipboard{}.SetImagePNG(data)
				}
				if err != nil {
					s.log.Printf("pin copy: %v", err)
				}
			})
		}()
		return
	}
	if err := (systemClipboard{}).SetText(p.Text()); err != nil {
		s.log.Printf("pin copy: %v", err)
	}
}

// savePin saves a pin's picture at the configured quality, or its text.
func (s *Shell) savePin(p *pin.Pin) {
	if img := p.Image(); img != nil {
		s.saveImageFile(img)
		return
	}
	s.saveText(p.Text())
}

// pinScreen is where a pin from the history opens: the display under the
// pointer (else the primary one), and the primary display's scale, which
// pictures in the history are shown at.
func pinScreen() (screen platform.Rect, scale float64) {
	screen, scale = platform.Rect{W: 1280, H: 800}, 1
	displays, err := platform.Displays()
	if err != nil || len(displays) == 0 {
		return screen, scale
	}
	x, y := platform.CursorPosition()
	screen = displays[0].Bounds
	under := false
	for _, d := range displays {
		if d.Primary {
			scale = d.Scale
			if !under {
				screen = d.Bounds
			}
		}
		if d.Bounds.Contains(x, y) {
			screen, under = d.Bounds, true
		}
	}
	return screen, scale
}

// pinHistoryItem pins a history item in the middle of the screen under the
// pointer, each further pin offset a little. A picture is read in the
// background.
func (s *Shell) pinHistoryItem(store *history.Store, it history.Item) {
	screen, scale := pinScreen()
	if it.Kind == history.KindText {
		lines, w, h := pin.TextLayout(pin.Measure(), it.Text, screen)
		s.openPin(pin.NewText(s.nextPinTitle(), it.Text, lines, pin.Centered(screen, w, h, len(s.pins)), sui.Current()))
		return
	}
	go func() {
		img, err := store.LoadImage(it.ID)
		platform.PostMain(func() {
			if err != nil {
				s.log.Printf("pin: %v", err)
				return
			}
			w, h := pin.ImageSize(img, scale, screen)
			s.openPin(pin.NewImage(s.nextPinTitle(), img, pin.Centered(screen, w, h, len(s.pins)), sui.Current()))
		})
	}()
}
