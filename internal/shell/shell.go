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
	"snaplite/internal/pin"
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

	busy     bool // a capture session is running
	overlays []*app.Window
	// hotkeyOff unregisters each registered hotkey (capture, history); nil
	// while that hotkey is not registered.
	hotkeyOff [2]func()
	// hotkeyProblem is shown in the tray when a hotkey could not be registered;
	// hotkeyProblems has one entry per hotkey for the settings window.
	hotkeyProblem  string
	hotkeyProblems [2]string
	// settings is the open settings window, if any.
	settings *settingsWindow
	// permission is the open screen-recording explanation window, if any.
	permission *app.Window
	// pins are the open pins; pinCount numbers their window titles.
	pins     []*pinWindow
	pinCount int
	// saveDialog asks where to save a file (app.SaveFile); tests replace it.
	saveDialog func(app.SaveFileOptions) (string, error)

	extra extensions
}

// New makes the shell; Start brings it up.
func New(a *app.Application, cfg *config.Config) *Shell {
	return &Shell{app: a, cfg: cfg, log: openLog(), saveDialog: nativeSaveDialog}
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

// Quit unregisters everything, closes the pins and ends the application.
func (s *Shell) Quit() {
	s.suspendHotkeys()
	s.CloseAllPins()
	s.stopExtensions()
	s.app.Quit()
}

// hotkeyActions are what the two configured hotkeys do, in HotkeySpecs order.
func (s *Shell) hotkeyActions() [2]func() {
	return [2]func(){
		s.StartCapture,
		func() { s.ShowHistory(app.StatusAnchor{}) },
	}
}

// registerHotkeys registers every configured hotkey that is not registered
// yet. It returns one problem text per hotkey ("" when it is registered) and
// shows the first problem in the tray.
func (s *Shell) registerHotkeys() [2]string {
	var problems [2]string
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
func (s *Shell) StartCapture() {
	defer s.guard("capture")
	if s.busy {
		return
	}
	s.busy = true
	started := time.Now()
	go func() {
		session, err := capture.Begin(capture.Native{})
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
	if res.Kind == overlay.Cancel {
		session.Release()
		s.busy = false
		platform.PrefetchCaptureContent()
		return
	}
	frame := res.Frame
	crop := frame.Crop(res.Sel)
	scale := frame.Scale()
	screen, sel := frame.Display.Bounds, frame.ScreenRect(res.SelWindow)
	session.Release()
	res.Frame = nil // the goroutine needs only the crop, not the whole capture
	oneX := s.cfg.Capture.Scale == config.Scale1x
	quality := s.cfg.JPEGQuality()
	go func() {
		out, err := render(crop, res, scale, oneX, quality)
		platform.PostMain(func() {
			defer s.guard("deliver")
			s.busy = false
			if err != nil {
				s.log.Printf("encode: %v", err)
				return
			}
			switch res.Kind {
			case overlay.Pin:
				// Only pinned: neither copied nor recorded in the history.
				w, h := pin.ImageSize(out.img, out.scale, screen)
				s.openPin(pin.NewImage(s.nextPinTitle(), out.img, pin.Within(sel, w, h), sui.Current()))
			case overlay.Save:
				s.saveImage(out.img, out.png, out.file, out.ext)
			default:
				s.deliver(out.img, out.png)
			}
		})
	}()
	platform.PrefetchCaptureContent()
}

// output is a finished screenshot.
type output struct {
	img *image.RGBA
	// scale is the picture's physical pixels per logical pixel: the
	// display's, or 1 once shrunk to 1×.
	scale float64
	// png goes to the clipboard and the history (not made for a pin); file
	// is what a save writes, encoded as ext.
	png, file []byte
	ext       string
}

// render composes the annotations onto the selection's crop, shrinks it to
// 1× when asked, and encodes what the result needs: PNG for the clipboard
// and the history, and for a save also the file at jpegQuality (0: PNG).
func render(crop *image.RGBA, res overlay.Result, scale float64, oneX bool, jpegQuality int) (output, error) {
	out := output{img: raster.ComposeOffset(crop, res.Shapes, res.Sel.Min), scale: scale}
	if oneX {
		out.img, out.scale = raster.Downscale(out.img, scale), 1
	}
	var err error
	switch res.Kind {
	case overlay.Done:
		out.png, err = raster.EncodePNG(out.img)
	case overlay.Save:
		if out.png, err = raster.EncodePNG(out.img); err == nil {
			out.file, out.ext, err = raster.EncodeFile(out.img, jpegQuality)
		}
	}
	return out, err
}

// deliver puts a finished picture on the clipboard and into the history.
func (s *Shell) deliver(img *image.RGBA, data []byte) {
	if err := platform.SetClipboardPNG(data); err != nil {
		s.log.Printf("clipboard: %v", err)
	}
	s.addToHistory(img, data)
}

// SaveName is the suggested file name for a screenshot taken at t, with the
// extension "png" or "jpg".
func SaveName(t time.Time, ext string) string { return t.Format("简截_20060102_150405.") + ext }

// TextSaveName is the suggested file name for a saved clipboard text.
func TextSaveName(t time.Time) string { return t.Format("剪切板_20060102_150405.txt") }

// imageSaveOptions is the save dialog for a picture encoded as ext ("png"
// or "jpg", raster.EncodeFile's).
func imageSaveOptions(ext string, t time.Time) app.SaveFileOptions {
	filter := app.FileFilter{Name: "PNG 图片", Pattern: "*.png"}
	if ext == "jpg" {
		filter = app.FileFilter{Name: "JPEG 图片", Pattern: "*.jpg"}
	}
	return app.SaveFileOptions{Title: "保存截图", SuggestedName: SaveName(t, ext), DefaultExtension: ext, Filters: []app.FileFilter{filter}}
}

// textSaveOptions is the save dialog for a text, written as UTF-8.
func textSaveOptions(t time.Time) app.SaveFileOptions {
	return app.SaveFileOptions{Title: "保存文字", SuggestedName: TextSaveName(t), DefaultExtension: "txt",
		Filters: []app.FileFilter{{Name: "文本文件", Pattern: "*.txt"}}}
}

// fileExt is the extension raster.EncodeFile gives a JPEG quality (0: PNG).
func fileExt(jpegQuality int) string {
	if jpegQuality > 0 {
		return "jpg"
	}
	return "png"
}

// nativeSaveDialog brings SnapLite forward and shows the system save dialog.
func nativeSaveDialog(opts app.SaveFileOptions) (string, error) {
	platform.ActivateApp()
	return app.SaveFile(opts)
}

// askSavePath shows the save dialog and remembers the folder; false when the
// user cancels, which is not an error.
func (s *Shell) askSavePath(opts app.SaveFileOptions) (string, bool) {
	path, err := s.saveDialog(opts)
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

// saveImage asks where to save, writes the file (encoded as ext) and also
// copies the picture as PNG.
func (s *Shell) saveImage(img *image.RGBA, data, file []byte, ext string) {
	path, ok := s.askSavePath(imageSaveOptions(ext, time.Now()))
	if !ok {
		return
	}
	if err := writeFile(path, file); err != nil {
		s.log.Printf("save: %v", err)
		return
	}
	s.deliver(img, data)
}

// saveImageFile asks where to save a picture and writes it at the configured
// quality, encoding in the background. Nothing is copied.
func (s *Shell) saveImageFile(img *image.RGBA) {
	quality := s.cfg.JPEGQuality()
	path, ok := s.askSavePath(imageSaveOptions(fileExt(quality), time.Now()))
	if !ok {
		return
	}
	go func() {
		data, _, err := raster.EncodeFile(img, quality)
		if err == nil {
			err = writeFile(path, data)
		}
		if err != nil {
			s.log.Printf("save: %v", err)
		}
	}()
}

// saveText asks where to save a text and writes it as UTF-8.
func (s *Shell) saveText(text string) {
	path, ok := s.askSavePath(textSaveOptions(time.Now()))
	if !ok {
		return
	}
	if err := writeFile(path, []byte(text)); err != nil {
		s.log.Printf("save: %v", err)
	}
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
