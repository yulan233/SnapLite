package shell

import (
	"runtime"

	"github.com/vivaui/viva/app"

	"snaplite/internal/annotate"
	sui "snaplite/internal/ui"
)

// trayIcon is the viewfinder icon: a template image on macOS, mid grey
// (visible on light and dark taskbars) on Windows.
func trayIcon() app.StatusIcon {
	if runtime.GOOS == "darwin" {
		return app.StatusIcon{Size: 32, RGBA: sui.TrayIcon(32, annotate.Color{A: 255}), Template: true}
	}
	return app.StatusIcon{Size: 32, RGBA: sui.TrayIcon(32, annotate.Color{R: 0x5F, G: 0x63, B: 0x68, A: 255})}
}

func (s *Shell) addTray() error {
	tooltip := "简截"
	if s.hotkeyProblem != "" {
		tooltip = "简截 · " + s.hotkeyProblem
	}
	item, err := s.app.AddStatusItem(app.StatusItemConfig{Tooltip: tooltip, Icon: trayIcon(), Menu: s.menu})
	s.tray = item
	return err
}

// withShortcut puts the shortcut text after a menu title; Viva's menus do not
// parse shortcut characters.
func withShortcut(title, spec string) string {
	if text := hotkeyText(spec); text != "" {
		return title + "    " + text
	}
	return title
}

// menu is built each time the menu opens.
func (s *Shell) menu() []app.MenuItem {
	var items []app.MenuItem
	if s.hotkeyProblem != "" {
		items = append(items, app.MenuItem{Title: s.hotkeyProblem, Disabled: true}, app.MenuItem{Separator: true})
	}
	items = append(items,
		app.MenuItem{Title: withShortcut("截图", s.cfg.Hotkeys.Capture), Disabled: s.busy, OnSelect: s.StartCapture},
		app.MenuItem{Title: withShortcut("剪切板历史", s.cfg.Hotkeys.History), OnSelect: func() { s.ShowHistory(app.StatusAnchor{}) }},
		app.MenuItem{Title: "关闭全部钉图", Disabled: len(s.pins) == 0, OnSelect: s.CloseAllPins},
		app.MenuItem{Separator: true},
	)
	items = append(items, s.extraMenu()...)
	items = append(items,
		app.MenuItem{Title: "设置…", OnSelect: s.ShowSettings},
		app.MenuItem{Title: "打开截图保存目录", OnSelect: s.OpenSaveDirectory},
		app.MenuItem{Separator: true},
		app.MenuItem{Title: "退出简截", OnSelect: s.Quit},
	)
	return items
}
