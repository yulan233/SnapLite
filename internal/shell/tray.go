package shell

import (
	"runtime"

	"github.com/vivaui/viva/app"

	"snaplite/internal/annotate"
	"snaplite/internal/i18n"
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
	item, err := s.app.AddStatusItem(app.StatusItemConfig{Tooltip: s.trayTooltip(), Icon: trayIcon(), Menu: s.menu})
	s.tray = item
	return err
}

// trayTooltip is the app's name, and the hotkey problem when there is one.
func (s *Shell) trayTooltip() string {
	if s.hotkeyProblem != "" {
		return i18n.AppName() + " · " + s.hotkeyProblem
	}
	return i18n.AppName()
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
		app.MenuItem{Title: withShortcut(i18n.T("截图", "Capture"), s.cfg.Hotkeys.Capture), Disabled: s.busy, OnSelect: s.StartCapture},
		app.MenuItem{Title: withShortcut(i18n.T("剪切板历史", "Clipboard History"), s.cfg.Hotkeys.History), OnSelect: func() { s.ShowHistory(app.StatusAnchor{}) }},
		app.MenuItem{Title: i18n.T("关闭全部钉图", "Close All Pins"), Disabled: len(s.pins) == 0, OnSelect: s.CloseAllPins},
		app.MenuItem{Separator: true},
	)
	items = append(items, s.extraMenu()...)
	items = append(items,
		app.MenuItem{Title: i18n.T("设置…", "Settings…"), OnSelect: s.ShowSettings},
		app.MenuItem{Title: i18n.T("打开截图保存目录", "Open Screenshots Folder"), OnSelect: s.OpenSaveDirectory},
		app.MenuItem{Separator: true},
		app.MenuItem{Title: i18n.T("退出简截", "Quit SnapLite"), OnSelect: s.Quit},
	)
	return items
}
