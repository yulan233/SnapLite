package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"snaplite/internal/platform"
)

// Config is config.json. Fields missing from the file take their defaults.
type Config struct {
	Hotkeys struct {
		Capture string `json:"capture"`
		History string `json:"history"`
	} `json:"hotkeys"`
	SaveDir     string `json:"saveDir"`
	LastSaveDir string `json:"lastSaveDir"`
	History     struct {
		Enabled    bool  `json:"enabled"`
		MaxItems   int   `json:"maxItems"`
		MaxBytes   int64 `json:"maxBytes"`
		RecordText bool  `json:"recordText"`
		// ClickAction is what clicking a history item does: "copy" puts it on
		// the clipboard, "paste" also pastes it into the application in front.
		ClickAction string `json:"clickAction"`
	} `json:"history"`
	// Capture is how screenshots come out. Quality only applies to saved
	// files: the clipboard, the history and pins always get lossless PNG.
	Capture struct {
		Quality string `json:"quality"` // QualityPNG, QualityHigh, QualityMedium, QualityLow
		Scale   string `json:"scale"`   // ScaleOriginal, Scale1x
	} `json:"capture"`

	path string
}

// History click actions.
const (
	ClickCopy  = "copy"
	ClickPaste = "paste"
)

// Capture qualities: PNG, or JPEG at 92, 80 and 60.
const (
	QualityPNG    = "png"
	QualityHigh   = "high"
	QualityMedium = "medium"
	QualityLow    = "low"
)

// Qualities are the capture qualities in the order the settings window lists them.
var Qualities = []string{QualityPNG, QualityHigh, QualityMedium, QualityLow}

// Capture scales: the captured physical pixels, or one pixel per logical
// pixel (the picture shrunk by the display's scale).
const (
	ScaleOriginal = "original"
	Scale1x       = "1x"
)

// JPEGQuality is the JPEG quality a saved file is encoded with, 0 for PNG.
func (c *Config) JPEGQuality() int {
	switch c.Capture.Quality {
	case QualityHigh:
		return 92
	case QualityMedium:
		return 80
	case QualityLow:
		return 60
	}
	return 0
}

// MaxItemsChoices and MaxBytesChoices are the history limits the settings
// window offers.
var (
	MaxItemsChoices = []int{20, 50, 100, 200, 500, 1000}
	MaxBytesChoices = []int64{100 << 20, 500 << 20, 1 << 30, 2 << 30}
)

// Default returns the built-in configuration.
func Default() *Config { return DefaultFor(runtime.GOOS) }

// DefaultFor is the built-in configuration of one platform: Option+F1/F2 on
// macOS, right Alt+F1/F2 on Windows.
func DefaultFor(goos string) *Config {
	c := &Config{}
	alt := "RightAlt"
	if goos == "darwin" {
		alt = "Option"
	}
	c.Hotkeys.Capture = alt + "+F1"
	c.Hotkeys.History = alt + "+F2"
	c.History.Enabled = true
	c.History.MaxItems = 100
	c.History.MaxBytes = 500 << 20
	c.History.RecordText = true
	c.History.ClickAction = ClickCopy
	c.Capture.Quality = QualityPNG
	c.Capture.Scale = ScaleOriginal
	return c
}

// DataDir is SnapLite's per-user data directory:
// ~/Library/Application Support/SnapLite on macOS, %APPDATA%\SnapLite on Windows.
func DataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "SnapLite")
}

// DefaultSaveDir is the 简截 folder in the user's Pictures folder.
func DefaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "简截")
	}
	return filepath.Join(home, "Pictures", "简截")
}

// Load reads config.json from DataDir, writing the defaults when it is missing.
func Load() (*Config, error) { return LoadFrom(filepath.Join(DataDir(), "config.json")) }

// LoadFrom reads a configuration file. A missing file yields the defaults,
// which are written out; a malformed file yields the defaults and the error.
func LoadFrom(path string) (*Config, error) {
	c := Default()
	c.path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, c.Save()
	}
	if err != nil {
		return c, err
	}
	// Unmarshal over the defaults: absent fields keep them. A file that sets
	// enabled:false is honoured because the field is present.
	if err := json.Unmarshal(data, c); err != nil {
		d := Default()
		d.path = path
		return d, fmt.Errorf("config: %s: %w", path, err)
	}
	if c.normalize() {
		return c, c.Save()
	}
	return c, nil
}

// Hotkeys earlier defaults wrote into config.json, per hotkey: 0.1.0's
// Cmd/Ctrl+Shift chords, and F3 for the history from when F2 was the long
// shot's. A file that still holds one gets the current default instead: the
// user never chose it.
var (
	legacyCapture = map[string]bool{"CmdOrCtrl+Shift+A": true}
	legacyHistory = map[string]bool{"CmdOrCtrl+Shift+V": true, "Option+F3": true, "RightAlt+F3": true}
)

// normalize replaces values that cannot work with the defaults. It reports
// whether anything changed, so LoadFrom can write the file back.
func (c *Config) normalize() bool {
	d := Default()
	changed := false
	for _, pair := range [...]struct {
		value  *string
		def    string
		legacy map[string]bool
		other  string // the other hotkey, which the default must not repeat
	}{
		{&c.Hotkeys.Capture, d.Hotkeys.Capture, legacyCapture, c.Hotkeys.History},
		{&c.Hotkeys.History, d.Hotkeys.History, legacyHistory, c.Hotkeys.Capture},
	} {
		if *pair.value == "" || pair.legacy[*pair.value] && pair.def != pair.other {
			*pair.value = pair.def
			changed = true
		}
	}
	if c.History.MaxItems <= 0 {
		c.History.MaxItems = d.History.MaxItems
	}
	if c.History.MaxBytes <= 0 {
		c.History.MaxBytes = d.History.MaxBytes
	}
	if c.History.ClickAction != ClickPaste {
		c.History.ClickAction = ClickCopy
	}
	switch c.Capture.Quality {
	case QualityPNG, QualityHigh, QualityMedium, QualityLow:
	default:
		c.Capture.Quality = QualityPNG
	}
	if c.Capture.Scale != Scale1x {
		c.Capture.Scale = ScaleOriginal
	}
	return changed
}

// Path is the file this configuration is saved to.
func (c *Config) Path() string { return c.path }

// SaveDirectory is where the save dialog should start: the last directory
// used, else the configured one, else the default.
func (c *Config) SaveDirectory() string {
	switch {
	case c.LastSaveDir != "":
		return c.LastSaveDir
	case c.SaveDir != "":
		return c.SaveDir
	}
	return DefaultSaveDir()
}

// Save writes the file atomically: a temporary file renamed over the old one.
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config: no path")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// ParseHotkey parses "CmdOrCtrl+Shift+A" or "Option+F1" style strings,
// case-insensitively. Modifiers: CmdOrCtrl (CommandOrControl), Cmd (Command),
// Ctrl (Control), Shift, Alt (Option), RightAlt (RAlt; either Option key on
// macOS). The key is one letter, digit or F1–F12 and must come last. A letter
// or digit needs Cmd, Ctrl or an Alt; a function key may stand alone.
func ParseHotkey(s string) (platform.Hotkey, error) {
	var h platform.Hotkey
	parts := strings.Split(s, "+")
	if len(parts) < 2 && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "f") {
		return h, fmt.Errorf("config: hotkey %q needs a modifier and a key", s)
	}
	for i, raw := range parts {
		part := strings.ToLower(strings.TrimSpace(raw))
		if i == len(parts)-1 {
			key, ok := parseKey(part)
			if !ok {
				return h, fmt.Errorf("config: hotkey %q must end in a letter, digit or F1–F12", s)
			}
			h.Key = key
			break
		}
		switch part {
		case "cmdorctrl", "commandorcontrol", "cmd", "command", "super", "meta":
			h.Mods |= platform.ModCmdOrCtrl
		case "ctrl", "control":
			h.Mods |= platform.ModCtrl
		case "shift":
			h.Mods |= platform.ModShift
		case "alt", "option", "opt":
			h.Mods |= platform.ModAlt
		case "rightalt", "ralt", "rightoption", "altgr":
			h.Mods |= platform.ModRightAlt
		default:
			return h, fmt.Errorf("config: hotkey %q has unknown modifier %q", s, raw)
		}
	}
	if _, function := platform.FunctionKey(h.Key); !function && h.Mods&^platform.ModShift == 0 {
		return h, fmt.Errorf("config: hotkey %q needs Cmd, Ctrl or Alt", s)
	}
	return h, nil
}

// SpecFor writes a hotkey the way config.json holds it on one platform, the
// inverse of ParseHotkey: "Option+F1" on macOS, "RightAlt+F1" or
// "CmdOrCtrl+Shift+A" on Windows.
func SpecFor(goos string, h platform.Hotkey) string {
	var parts []string
	if h.Mods&platform.ModCmdOrCtrl != 0 {
		parts = append(parts, "CmdOrCtrl")
	}
	if h.Mods&platform.ModCtrl != 0 && (goos == "darwin" || h.Mods&platform.ModCmdOrCtrl == 0) {
		parts = append(parts, "Ctrl")
	}
	switch {
	case h.Mods&platform.ModRightAlt != 0 && goos != "darwin":
		parts = append(parts, "RightAlt")
	case h.Mods&(platform.ModAlt|platform.ModRightAlt) != 0 && goos == "darwin":
		parts = append(parts, "Option")
	case h.Mods&platform.ModAlt != 0:
		parts = append(parts, "Alt")
	}
	if h.Mods&platform.ModShift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, keyName(h.Key)), "+")
}

// HotkeySpecs are the two configured hotkeys: capture and history.
func (c *Config) HotkeySpecs() [2]*string {
	return [2]*string{&c.Hotkeys.Capture, &c.Hotkeys.History}
}

// parseKey reads a lower-case key name: one letter or digit, or f1..f12.
func parseKey(part string) (rune, bool) {
	if len(part) >= 2 && part[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(part[1:], "%d", &n); err == nil && fmt.Sprint(n) == part[1:] && n >= 1 && n <= 12 {
			return platform.KeyF1 + rune(n-1), true
		}
		return 0, false
	}
	r := []rune(strings.ToUpper(part))
	if len(r) != 1 || !((r[0] >= 'A' && r[0] <= 'Z') || (r[0] >= '0' && r[0] <= '9')) {
		return 0, false
	}
	return r[0], true
}

// keyName is how a hotkey's key is written: "A", "1" or "F1".
func keyName(key rune) string {
	if n, ok := platform.FunctionKey(key); ok {
		return fmt.Sprintf("F%d", n)
	}
	return string(key)
}

// FormatHotkey is how the hotkey is shown on this platform.
func FormatHotkey(h platform.Hotkey) string { return FormatHotkeyFor(runtime.GOOS, h) }

// FormatHotkeyFor shows a hotkey as macOS ("⌘⇧A") or Windows ("Ctrl+Shift+A") does.
func FormatHotkeyFor(goos string, h platform.Hotkey) string {
	if goos == "darwin" {
		// The order the product documents use: ⌘⇧A.
		var b strings.Builder
		if h.Mods&platform.ModCmdOrCtrl != 0 {
			b.WriteString("⌘")
		}
		if h.Mods&platform.ModCtrl != 0 {
			b.WriteString("⌃")
		}
		if h.Mods&(platform.ModAlt|platform.ModRightAlt) != 0 {
			b.WriteString("⌥")
		}
		if h.Mods&platform.ModShift != 0 {
			b.WriteString("⇧")
		}
		b.WriteString(keyName(h.Key))
		return b.String()
	}
	var parts []string
	if h.Mods&(platform.ModCmdOrCtrl|platform.ModCtrl) != 0 {
		parts = append(parts, "Ctrl")
	}
	if h.Mods&platform.ModRightAlt != 0 {
		parts = append(parts, "右Alt")
	} else if h.Mods&platform.ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if h.Mods&platform.ModShift != 0 {
		parts = append(parts, "Shift")
	}
	parts = append(parts, keyName(h.Key))
	return strings.Join(parts, "+")
}
