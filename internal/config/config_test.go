package config

import (
	"os"
	"path/filepath"
	"testing"

	"snaplite/internal/platform"
)

func TestLoadWritesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hotkeys.Capture != Default().Hotkeys.Capture || c.History.MaxItems != 100 || c.History.MaxBytes != 500<<20 || !c.History.Enabled || c.LongShot.Mode != "auto" || c.LongShot.MaxHeight != 30000 {
		t.Fatalf("defaults = %+v", c)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("defaults were not written: %v", err)
	}
}

func TestLoadKeepsDefaultsForMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"history":{"enabled":false,"maxItems":20},"saveDir":"/tmp/x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.History.Enabled || c.History.MaxItems != 20 || c.SaveDir != "/tmp/x" {
		t.Fatalf("explicit values lost: %+v", c.History)
	}
	if c.Hotkeys.History != Default().Hotkeys.History || c.History.MaxBytes != 500<<20 || !c.History.RecordText {
		t.Fatalf("missing values not defaulted: %+v", c)
	}
	if c.SaveDirectory() != "/tmp/x" {
		t.Fatal("SaveDirectory should fall back to saveDir")
	}
	c.LastSaveDir = "/tmp/y"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := LoadFrom(path)
	if err != nil || again.SaveDirectory() != "/tmp/y" {
		t.Fatalf("round trip: %v %+v", err, again)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func TestLoadMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte(`{nope`), 0o644)
	c, err := LoadFrom(path)
	if err == nil || c == nil || c.History.MaxItems != 100 {
		t.Fatalf("malformed file: %v %+v", err, c)
	}
}

func TestParseHotkey(t *testing.T) {
	good := map[string]platform.Hotkey{
		"CmdOrCtrl+Shift+A":   {Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: 'A'},
		"cmdorctrl+shift+l":   {Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: 'L'},
		"Ctrl+Alt+5":          {Mods: platform.ModCtrl | platform.ModAlt, Key: '5'},
		" Command + Option+v": {Mods: platform.ModCmdOrCtrl | platform.ModAlt, Key: 'V'},
		"Option+F1":           {Mods: platform.ModAlt, Key: platform.KeyF1},
		"RightAlt+f3":         {Mods: platform.ModRightAlt, Key: platform.KeyF3},
		"Ctrl+F12":            {Mods: platform.ModCtrl, Key: platform.KeyF12},
		"F2":                  {Key: platform.KeyF2},
	}
	for s, want := range good {
		got, err := ParseHotkey(s)
		if err != nil || got != want {
			t.Errorf("ParseHotkey(%q) = %+v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "A", "Shift+A", "Ctrl+Shift", "Ctrl+F13", "Ctrl+F0", "Ctrl+F01", "Ctrl+Fx", "Hyper+A", "Ctrl+AB", "Ctrl+!", "Ctrl++"} {
		if _, err := ParseHotkey(s); err == nil {
			t.Errorf("ParseHotkey(%q) should fail", s)
		}
	}
}

func TestFormatHotkey(t *testing.T) {
	h := platform.Hotkey{Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: 'A'}
	if got := FormatHotkeyFor("darwin", h); got != "⌘⇧A" {
		t.Errorf("darwin = %q", got)
	}
	if got := FormatHotkeyFor("windows", h); got != "Ctrl+Shift+A" {
		t.Errorf("windows = %q", got)
	}
	if got := FormatHotkeyFor("windows", platform.Hotkey{Mods: platform.ModAlt | platform.ModCtrl, Key: '1'}); got != "Ctrl+Alt+1" {
		t.Errorf("windows alt = %q", got)
	}
	f1 := platform.Hotkey{Mods: platform.ModAlt, Key: platform.KeyF1}
	if got := FormatHotkeyFor("darwin", f1); got != "⌥F1" {
		t.Errorf("darwin F1 = %q", got)
	}
	if got := FormatHotkeyFor("windows", platform.Hotkey{Mods: platform.ModRightAlt, Key: platform.KeyF2}); got != "右Alt+F2" {
		t.Errorf("windows right alt = %q", got)
	}
}

func TestPlatformDefaults(t *testing.T) {
	mac, win := DefaultFor("darwin").Hotkeys, DefaultFor("windows").Hotkeys
	if mac.Capture != "Option+F1" || mac.LongShot != "Option+F2" || mac.History != "Option+F3" {
		t.Errorf("darwin defaults = %+v", mac)
	}
	if win.Capture != "RightAlt+F1" || win.LongShot != "RightAlt+F2" || win.History != "RightAlt+F3" {
		t.Errorf("windows defaults = %+v", win)
	}
	for _, s := range []string{mac.Capture, mac.LongShot, mac.History, win.Capture, win.LongShot, win.History} {
		if _, err := ParseHotkey(s); err != nil {
			t.Errorf("default %q does not parse: %v", s, err)
		}
	}
}

func TestLoadMigratesOldDefaultHotkeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"hotkeys":{"capture":"CmdOrCtrl+Shift+A","longshot":"Ctrl+Alt+L","history":"CmdOrCtrl+Shift+V"}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Default().Hotkeys
	if c.Hotkeys.Capture != d.Capture || c.Hotkeys.History != d.History {
		t.Fatalf("old defaults kept: %+v", c.Hotkeys)
	}
	if c.Hotkeys.LongShot != "Ctrl+Alt+L" {
		t.Fatalf("a hotkey the user chose was replaced: %q", c.Hotkeys.LongShot)
	}
	again, err := LoadFrom(path)
	if err != nil || again.Hotkeys.Capture != d.Capture {
		t.Fatalf("migration was not written back: %v %+v", err, again.Hotkeys)
	}
}

func TestSpecForRoundTrips(t *testing.T) {
	cases := []struct {
		goos string
		h    platform.Hotkey
		want string
	}{
		{"darwin", platform.Hotkey{Mods: platform.ModAlt, Key: platform.KeyF1}, "Option+F1"},
		{"darwin", platform.Hotkey{Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: 'A'}, "CmdOrCtrl+Shift+A"},
		{"darwin", platform.Hotkey{Mods: platform.ModCtrl | platform.ModAlt, Key: '5'}, "Ctrl+Option+5"},
		{"windows", platform.Hotkey{Mods: platform.ModRightAlt, Key: platform.KeyF2}, "RightAlt+F2"},
		{"windows", platform.Hotkey{Mods: platform.ModCmdOrCtrl | platform.ModAlt, Key: 'X'}, "CmdOrCtrl+Alt+X"},
		{"windows", platform.Hotkey{Key: platform.KeyF9}, "F9"},
	}
	for _, c := range cases {
		got := SpecFor(c.goos, c.h)
		if got != c.want {
			t.Errorf("SpecFor(%s, %+v) = %q, want %q", c.goos, c.h, got, c.want)
			continue
		}
		back, err := ParseHotkey(got)
		if err != nil || back != c.h {
			t.Errorf("ParseHotkey(%q) = %+v, %v; want %+v", got, back, err, c.h)
		}
	}
}
