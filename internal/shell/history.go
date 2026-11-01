package shell

import (
	"errors"
	"image"
	"path/filepath"
	"runtime"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/history"
	"snaplite/internal/i18n"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

// extensions is the shell state of the clipboard history.
type extensions struct {
	history     *history.Store
	stopWatch   func()
	paused      bool
	panel       *history.Panel
	panelWindow *app.Window
}

// systemClipboard is history.Clipboard over Viva (text) and the platform
// layer (images, which Viva cannot write).
type systemClipboard struct{}

// SetText leaves the clipboard alone when it already holds the text, and marks
// the write as SnapLite's own so the watcher does not record it again.
func (systemClipboard) SetText(text string) error {
	if current, err := app.ClipboardText(); err == nil && current == text && !app.HasClipboardImage() {
		return nil
	}
	if err := app.SetClipboardText(text); err != nil {
		return err
	}
	platform.MarkClipboardOwn()
	return nil
}

// SetImagePNG writes through the platform layer, which records its own change.
func (systemClipboard) SetImagePNG(data []byte) error { return platform.SetClipboardPNG(data) }

func (s *Shell) startExtensions() {
	store, err := history.Open(filepath.Join(config.DataDir(), "history"), history.Limits{
		MaxItems: s.cfg.History.MaxItems, MaxBytes: s.cfg.History.MaxBytes,
	})
	if err != nil {
		s.log.Printf("history: %v", err)
		return
	}
	s.extra.history = store
	s.setWatching(s.cfg.History.Enabled)
}

// setWatching starts or stops watching the clipboard for the history.
func (s *Shell) setWatching(on bool) {
	switch {
	case on && s.extra.stopWatch == nil:
		stop, err := platform.WatchClipboard(s.clipboardChanged)
		if err != nil {
			s.log.Printf("clipboard watch: %v", err)
			return
		}
		s.extra.stopWatch = stop
	case !on && s.extra.stopWatch != nil:
		s.extra.stopWatch()
		s.extra.stopWatch = nil
	}
}

func (s *Shell) stopExtensions() {
	if s.extra.stopWatch != nil {
		s.extra.stopWatch()
		s.extra.stopWatch = nil
	}
	if s.extra.history != nil {
		s.extra.history.Close(time.Second)
	}
}

// clipboardChanged runs on the event-loop thread when the clipboard changed
// (docs/05 §8.3).
func (s *Shell) clipboardChanged() {
	defer s.guard("clipboard")
	store := s.extra.history
	if store == nil || s.extra.paused || platform.OwnClipboardChange() || platform.ClipboardConcealed() {
		return
	}
	if app.HasClipboardImage() {
		data, err := app.ClipboardImagePNG()
		if err != nil {
			return
		}
		go func() {
			img, err := raster.DecodePNG(data)
			if err == nil {
				_, _ = store.AddImagePNG(img, data)
			}
		}()
		return
	}
	if !s.cfg.History.RecordText {
		return
	}
	if text, err := app.ClipboardText(); err == nil && text != "" {
		_, _ = store.AddText(text)
	}
}

// addToHistory records SnapLite's own screenshot directly; the watcher skips
// the clipboard change it causes.
func (s *Shell) addToHistory(img *image.RGBA, data []byte) {
	if store := s.extra.history; store != nil && s.cfg.History.Enabled {
		go func() { _, _ = store.AddImagePNG(img, data) }()
	}
}

// ShowHistory toggles the history panel.
func (s *Shell) ShowHistory(anchor app.StatusAnchor) {
	defer s.guard("history")
	store := s.extra.history
	if store == nil {
		return
	}
	if w := s.extra.panelWindow; w != nil && !w.Closed() {
		w.Close()
		return
	}
	// Remember where the user was, so a paste goes back there.
	platform.SaveForegroundApp()
	panel := history.NewPanel(store, systemClipboard{}, sui.Current())
	panel.ClickPastes = s.cfg.History.ClickAction == config.ClickPaste
	if runtime.GOOS != "darwin" {
		panel.Mod = "Ctrl+"
	}
	config := panel.Config(anchor)
	closed := config.OnClosed
	config.OnClosed = func() {
		closed()
		store.SetOnChange(nil)
		s.extra.panel, s.extra.panelWindow = nil, nil
	}
	w, err := s.app.OpenWindow(config, new(vui.Canvas))
	if err != nil {
		s.log.Printf("history panel: %v", err)
		return
	}
	panel.OnClose = w.Close
	panel.OnPaste = func() {
		w.Close()
		s.pasteToFront()
	}
	// The panel closes when it loses focus anyway; it is closed first so the
	// pin or the save dialog comes up on its own.
	panel.OnPin = func(it history.Item) {
		w.Close()
		s.pinHistoryItem(store, it)
	}
	panel.OnSave = func(it history.Item) {
		w.Close()
		platform.PostMain(func() { s.saveHistoryItem(store, it) })
	}
	store.SetOnChange(panel.Wake)
	s.extra.panel, s.extra.panelWindow = panel, w
	platform.ActivateApp()
}

// saveHistoryItem asks where to save a history item and writes it: a text as
// UTF-8, a picture at the configured quality (read and encoded in the
// background).
func (s *Shell) saveHistoryItem(store *history.Store, it history.Item) {
	defer s.guard("history save")
	quality := s.cfg.JPEGQuality()
	opts := textSaveOptions(time.Now())
	if it.Kind == history.KindImage {
		opts = imageSaveOptions(fileExt(quality), time.Now())
	}
	path, ok := s.askSavePath(opts)
	if !ok {
		return
	}
	go func() {
		data, err := historyFileData(store, it, quality)
		if err == nil {
			err = writeFile(path, data)
		}
		if err != nil {
			s.log.Printf("save: %v", err)
		}
	}()
}

// historyFileData is what saving a history item writes: the text as UTF-8, a
// picture's stored PNG as it is, or the picture re-encoded as JPEG at
// jpegQuality.
func historyFileData(store *history.Store, it history.Item, jpegQuality int) ([]byte, error) {
	if it.Kind == history.KindText {
		return []byte(it.Text), nil
	}
	if jpegQuality <= 0 {
		return store.ImagePNG(it.ID)
	}
	img, err := store.LoadImage(it.ID)
	if err != nil {
		return nil, err
	}
	return raster.EncodeJPEG(img, jpegQuality)
}

// pasteToFront pastes the clipboard into the application that was in front
// before the history panel opened. Without the Accessibility permission (macOS)
// the item stays copied and the system is asked to show its permission prompt.
func (s *Shell) pasteToFront() {
	err := platform.PasteToSavedApp()
	switch {
	case errors.Is(err, platform.ErrPermissionDenied):
		s.log.Print("paste: accessibility permission missing; the item is copied")
		platform.RequestAccessibilityPermission()
	case err != nil:
		s.log.Printf("paste: %v", err)
	}
}

func (s *Shell) extraMenu() []app.MenuItem {
	return []app.MenuItem{
		{Title: i18n.T("暂停记录", "Pause Recording"), Checked: s.extra.paused, Disabled: !s.cfg.History.Enabled, OnSelect: func() { s.extra.paused = !s.extra.paused }},
		{Title: i18n.T("开机启动", "Launch at Login"), Checked: platform.LaunchAtLogin(), OnSelect: func() {
			if err := platform.SetLaunchAtLogin(!platform.LaunchAtLogin()); err != nil {
				s.log.Printf("launch at login: %v", err)
			}
		}},
	}
}
