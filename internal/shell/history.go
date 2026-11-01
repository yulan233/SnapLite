package shell

import (
	"image"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/history"
	"snaplite/internal/platform"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

// extensions is the shell state of the clipboard history and long shots.
type extensions struct {
	history     *history.Store
	stopWatch   func()
	paused      bool
	panel       *history.Panel
	panelWindow *app.Window
	long        *longShotRun
}

// systemClipboard is history.Clipboard over Viva (text) and the platform
// layer (images, which Viva cannot write).
type systemClipboard struct{}

func (systemClipboard) SetText(text string) error     { return app.SetClipboardText(text) }
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
	if !s.cfg.History.Enabled {
		return
	}
	stop, err := platform.WatchClipboard(s.clipboardChanged)
	if err != nil {
		s.log.Printf("clipboard watch: %v", err)
		return
	}
	s.extra.stopWatch = stop
}

func (s *Shell) stopExtensions() {
	if s.extra.stopWatch != nil {
		s.extra.stopWatch()
		s.extra.stopWatch = nil
	}
	if s.extra.long != nil {
		s.extra.long.session.Cancel()
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
	panel := history.NewPanel(store, systemClipboard{}, sui.Current())
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
	store.SetOnChange(panel.Wake)
	s.extra.panel, s.extra.panelWindow = panel, w
	platform.ActivateApp()
}

func (s *Shell) extraMenu() []app.MenuItem {
	return []app.MenuItem{
		{Title: "暂停记录", Checked: s.extra.paused, Disabled: !s.cfg.History.Enabled, OnSelect: func() { s.extra.paused = !s.extra.paused }},
		{Title: "开机启动", Checked: platform.LaunchAtLogin(), OnSelect: func() {
			if err := platform.SetLaunchAtLogin(!platform.LaunchAtLogin()); err != nil {
				s.log.Printf("launch at login: %v", err)
			}
		}},
	}
}
