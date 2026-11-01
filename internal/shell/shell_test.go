package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	sui "snaplite/internal/ui"
)

func TestSaveName(t *testing.T) {
	when := time.Date(2026, 11, 1, 9, 30, 15, 0, time.Local)
	if got := SaveName(when, "png"); got != "简截_20261101_093015.png" {
		t.Fatalf("SaveName = %q", got)
	}
	if got := SaveName(when, "jpg"); got != "简截_20261101_093015.jpg" {
		t.Fatalf("SaveName jpg = %q", got)
	}
	if got := TextSaveName(when); got != "剪切板_20261101_093015.txt" {
		t.Fatalf("TextSaveName = %q", got)
	}
}

func TestPermissionWindow(t *testing.T) {
	opened, closed := 0, 0
	var canvas vui.Canvas
	d, err := app.NewDriver(permissionConfig(sui.Light(), func() { opened++ }, func() { closed++ }), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	found := false
	for _, l := range d.Snapshot().Labels {
		if strings.Contains(l.Value, "屏幕录制") {
			found = true
		}
	}
	if !found {
		t.Fatal("explanation missing")
	}
	for _, h := range d.Snapshot().Hits {
		if h.ID == permissionOpen {
			if err := d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
		}
	}
	if opened != 1 || closed != 0 {
		t.Fatalf("opened=%d closed=%d", opened, closed)
	}
}

func TestTrayIcon(t *testing.T) {
	icon := trayIcon()
	if icon.Size != 32 || len(icon.RGBA) != 32*32*4 {
		t.Fatal("icon size")
	}
	opaque := 0
	for i := 3; i < len(icon.RGBA); i += 4 {
		if icon.RGBA[i] > 128 {
			opaque++
		}
	}
	if opaque < 50 || opaque > 600 {
		t.Fatalf("icon coverage looks wrong: %d opaque pixels", opaque)
	}
	if withShortcut("截图", "CmdOrCtrl+Shift+A") == "截图" || withShortcut("截图", "bogus") != "截图" {
		t.Fatal("menu titles")
	}
}

func TestTrayMenu(t *testing.T) {
	s, _ := testShell(t, nil)
	find := func(title string) (app.MenuItem, bool) {
		for _, item := range s.menu() {
			if strings.HasPrefix(item.Title, title) {
				return item, true
			}
		}
		return app.MenuItem{}, false
	}
	if _, ok := find("长截图"); ok {
		t.Fatal("the long shot is gone from the tray")
	}
	item, ok := find("关闭全部钉图")
	if !ok || !item.Disabled {
		t.Fatal("关闭全部钉图 is there, disabled without pins")
	}
	s.pins = append(s.pins, &pinWindow{})
	if item, _ := find("关闭全部钉图"); item.Disabled {
		t.Fatal("关闭全部钉图 is enabled with a pin open")
	}
	if item, _ := find("剪切板历史"); !strings.Contains(item.Title, "⌥F2") && !strings.Contains(item.Title, "右Alt+F2") {
		t.Fatalf("history shortcut in the menu: %q", item.Title)
	}
}
