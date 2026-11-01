package shell

import (
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/capture"
	"snaplite/internal/config"
	"snaplite/internal/overlay"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

// Shell is the running application: tray, hotkeys and the flows they start.
// Every method runs on the event-loop thread.
type Shell struct {
	app  *app.Application
	cfg  *config.Config
	host *app.Window
	tray *app.StatusItem
	log  *log.Logger

	busy     bool // a capture or long-shot session is running
	overlays []*app.Window
	// hotkeyOff unregisters each registered hotkey (capture, long shot,
	// history); nil while that hotkey is not registered.
	hotkeyOff [3]func()
	// hotkeyProblem is shown in the tray when a hotkey could not be registered;
	// hotkeyProblems has one entry per hotkey for the settings window.
	hotkeyProblem  string
	hotkeyProblems [3]string
	// settings is the open settings window, if any.
	settings *settingsWindow
	// permission is the open screen-recording explanation window, if any.
	permission *app.Window

	extra extensions
}

// New makes the shell; Start brings it up.
func New(a *app.Application, cfg *config.Config) *Shell {
	return &Shell{app: a, cfg: cfg, log: openLog()}
}

// openLog writes to snaplite.log in the data directory, keeping it under 1 MB.
func openLog() *log.Logger {
	dir := config.DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return log.New(os.Stderr, "snaplite: ", log.LstdFlags)
	}
	path := filepath.Join(dir, "snaplite.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 1<<20 {
		if data, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(path, data[len(data)-512<<10:], 0o644)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(os.Stderr, "snaplite: ", log.LstdFlags)
	}
	return log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags)
}

// guard keeps a panic in one flow from taking the tray process down.
func (s *Shell) guard(flow string) {
	if r := recover(); r != nil {
		s.log.Printf("panic in %s: %v\n%s", flow, r, debug.Stack())
		s.busy = false
	}
}

// Fault logs a panic Viva caught at a native boundary.
func (s *Shell) Fault(f app.Fault) {
	s.log.Printf("fault in %s: %s\n%s", f.Where, f.Message, f.Stack)
}

// Start runs inside RunApplication's start callback.
func (s *Shell) Start() error {
	if err := platform.Init(); err != nil && !errors.Is(err, platform.ErrUnsupported) {
		s.log.Printf("platform init: %v", err)
	}
	// RunApplication needs an open window, and the application ends when the
	// last one closes, so a 1×1 window stays open, hidden.
	var hostCanvas vui.Canvas
	host, err := s.app.OpenWindow(app.Config{Title: "snaplite-host", WindowMode: app.WindowFrameless, Width: 1, Height: 1}, &hostCanvas)
	if err != nil {
		return err
	}
	host.Hide()
	s.host = host
	s.registerHotkeys()
	if err := s.addTray(); err != nil {
		s.log.Printf("tray: %v", err)
	}
	s.startExtensions()
	return nil
}

// Quit unregisters everything and ends the application.
func (s *Shell) Quit() {
	s.suspendHotkeys()
	s.stopExtensions()
	s.app.Quit()
}

// hotkeyActions are what the three configured hotkeys do, in HotkeySpecs order.
func (s *Shell) hotkeyActions() [3]func() {
	return [3]func(){
		func() { s.StartCapture(capture.ModeRegion) },
		func() { s.StartCapture(capture.ModeLongShot) },
		func() { s.ShowHistory(app.StatusAnchor{}) },
	}
}

// registerHotkeys registers every configured hotkey that is not registered
// yet. It returns one problem text per hotkey ("" when it is registered) and
// shows the first problem in the tray.
func (s *Shell) registerHotkeys() [3]string {
	var problems [3]string
	actions := s.hotkeyActions()
	for i, spec := range s.cfg.HotkeySpecs() {
		if s.hotkeyOff[i] != nil {
			continue
		}
		h, err := config.ParseHotkey(*spec)
		if err != nil {
			problems[i] = fmt.Sprintf("快捷键 %s 无效，可在 设置 中修改", *spec)
			s.log.Print(err)
			continue
		}
		unregister, err := platform.RegisterHotkey(h, actions[i])
		switch {
		case errors.Is(err, platform.ErrHotkeyTaken):
			problems[i] = fmt.Sprintf("快捷键 %s 已被其他程序占用，可在 设置 中修改", config.FormatHotkey(h))
		case err != nil:
			problems[i] = fmt.Sprintf("快捷键 %s 注册失败", config.FormatHotkey(h))
			s.log.Printf("hotkey %s: %v", *spec, err)
		default:
			s.hotkeyOff[i] = unregister
		}
	}
	s.hotkeyProblems = problems
	s.hotkeyProblem = ""
	for _, p := range problems {
		if p != "" {
			s.hotkeyProblem = p
			break
		}
	}
	if s.tray != nil {
		tooltip := "简截"
		if s.hotkeyProblem != "" {
			tooltip += " · " + s.hotkeyProblem
		}
		s.tray.SetTooltip(tooltip)
	}
	return problems
}

// suspendHotkeys unregisters every hotkey, so the settings window can receive
// the keys while it records one. The unregistering runs on a later loop turn
// (platform unregister functions post it), so registering again must also be
// posted, which keeps it after them in the main queue.
func (s *Shell) suspendHotkeys() {
	for i, off := range s.hotkeyOff {
		if off != nil {
			off()
			s.hotkeyOff[i] = nil
		}
	}
}

// hotkeyText is a configured hotkey as this platform shows it, "" if invalid.
func hotkeyText(spec string) string {
	h, err := config.ParseHotkey(spec)
	if err != nil {
		return ""
	}
	return config.FormatHotkey(h)
}

// StartCapture freezes every display and opens the overlays. Capturing runs
// on a goroutine (it blocks, and must not run on the macOS main thread); the
// overlays open when it is done.
func (s *Shell) StartCapture(mode capture.Mode) {
	defer s.guard("capture")
	if s.busy {
		return
	}
	s.busy = true
	started := time.Now()
	go func() {
		session, err := capture.Begin(capture.Native{}, mode)
		platform.PostMain(func() { s.openOverlays(session, err, started) })
	}()
}

func (s *Shell) openOverlays(session *capture.Session, err error, started time.Time) {
	defer s.guard("overlay")
	if err != nil {
		s.busy = false
		if errors.Is(err, platform.ErrPermissionDenied) {
			s.log.Printf("capture: %v (screen recording permission %v)", err, platform.HasScreenCapturePermission())
			s.showPermission()
			return
		}
		s.log.Printf("capture: %v", err)
		return
	}
	controller := overlay.NewController(session, sui.Current(), func(res overlay.Result) { s.overlayDone(session, res) })
	s.overlays = s.overlays[:0]
	for i := range session.Frames {
		w, err := s.app.OpenWindow(controller.Config(i), new(vui.Canvas))
		if err != nil {
			s.log.Printf("overlay window: %v", err)
			continue
		}
		s.overlays = append(s.overlays, w)
		frame := &session.Frames[i]
		s.place(overlay.WindowTitle(frame), frame.Display.Bounds, platform.RaiseOverlay)
	}
	if len(s.overlays) == 0 {
		s.busy = false
		session.Release()
		return
	}
	s.log.Printf("overlay shown after %v", time.Since(started).Round(time.Millisecond))
}

// place applies a platform window placement (RaiseOverlay, FloatWindow) to
// one of SnapLite's windows. A window an adapter creates asynchronously may
// not exist yet; then it is tried once more on the next loop turn.
func (s *Shell) place(title string, bounds platform.Rect, apply func(string, platform.Rect) error) {
	err := apply(title, bounds)
	if errors.Is(err, platform.ErrNotFound) {
		platform.PostMain(func() {
			if err := apply(title, bounds); err != nil {
				s.log.Printf("place %s: %v", title, err)
			}
		})
		return
	}
	if err != nil {
		s.log.Printf("place %s: %v", title, err)
	}
}

func (s *Shell) closeOverlays() {
	for _, w := range s.overlays {
		w.Close()
	}
	s.overlays = s.overlays[:0]
}

// overlayDone handles the overlay's result. The overlays close at once; the
// picture is composed and encoded in the background (docs/05 §6.4).
func (s *Shell) overlayDone(session *capture.Session, res overlay.Result) {
	defer s.guard("result")
	s.closeOverlays()
	switch res.Kind {
	case overlay.Cancel:
		session.Release()
		s.busy = false
		platform.PrefetchCaptureContent()
	case overlay.Done, overlay.Save:
		frame := res.Frame
		crop := frame.Crop(res.Sel)
		session.Release()
		go func() {
			img := raster.ComposeOffset(crop, res.Shapes, res.Sel.Min)
			data, err := raster.EncodePNG(img)
			platform.PostMain(func() {
				defer s.guard("deliver")
				s.busy = false
				if err != nil {
					s.log.Printf("encode: %v", err)
					return
				}
				if res.Kind == overlay.Save {
					s.saveImage(img, data)
					return
				}
				s.deliver(img, data)
			})
		}()
		platform.PrefetchCaptureContent()
	case overlay.LongShot:
		display := res.Frame.Display
		rect := res.Frame.ScreenRect(res.SelWindow)
		session.Release()
		s.startLongShot(display, rect)
	}
}

// deliver puts a finished picture on the clipboard and into the history.
func (s *Shell) deliver(img *image.RGBA, data []byte) {
	if err := platform.SetClipboardPNG(data); err != nil {
		s.log.Printf("clipboard: %v", err)
	}
	s.addToHistory(img, data)
}

// SaveName is the suggested file name for a screenshot taken at t.
func SaveName(t time.Time) string { return t.Format("简截_20060102_150405.png") }

// askSavePath shows the save dialog; false when the user cancels.
func (s *Shell) askSavePath() (string, bool) {
	platform.ActivateApp()
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            "保存截图",
		SuggestedName:    SaveName(time.Now()),
		DefaultExtension: "png",
		Filters:          []app.FileFilter{{Name: "PNG 图片", Pattern: "*.png"}},
	})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			s.log.Printf("save dialog: %v", err)
		}
		return "", false
	}
	s.cfg.LastSaveDir = filepath.Dir(path)
	if err := s.cfg.Save(); err != nil {
		s.log.Printf("config: %v", err)
	}
	return path, true
}

// saveImage asks where to save, writes the PNG and also copies it.
func (s *Shell) saveImage(img *image.RGBA, data []byte) {
	path, ok := s.askSavePath()
	if !ok {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		s.log.Printf("save: %v", err)
		return
	}
	s.deliver(img, data)
}

// OpenSaveDirectory reveals the screenshot folder, creating it first.
func (s *Shell) OpenSaveDirectory() {
	dir := s.cfg.SaveDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.log.Printf("save directory: %v", err)
		return
	}
	if err := platform.RevealDirectory(dir); err != nil {
		s.log.Printf("reveal: %v", err)
	}
}

func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
