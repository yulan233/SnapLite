package shell

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/i18n"
	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// fakeHotkeys is the shell side, applying everything at once.
type fakeHotkeys struct {
	specs     [2]string
	suspended bool
	taken     string // a spec that "another program" holds
	sets      int

	cfg        *config.Config
	applied    int
	saved      int
	login      bool
	loginErr   error
	granted    bool
	requested  int
	openedDirs int
}

func (f *fakeHotkeys) Config() *config.Config {
	if f.cfg == nil {
		f.cfg = config.DefaultFor("darwin")
	}
	return f.cfg
}
func (f *fakeHotkeys) ApplyHistory() { f.applied++ }
func (f *fakeHotkeys) SaveConfig()   { f.saved++ }
func (f *fakeHotkeys) SetLanguage(setting string) {
	f.Config().Language = setting
	i18n.Set(i18n.Resolve(setting, true))
	f.saved++
}
func (f *fakeHotkeys) LaunchAtLogin() bool        { return f.login }
func (f *fakeHotkeys) AccessibilityGranted() bool { return f.granted }
func (f *fakeHotkeys) RequestAccessibility()      { f.requested++ }
func (f *fakeHotkeys) OpenSaveDirectory()         { f.openedDirs++ }
func (f *fakeHotkeys) SetLaunchAtLogin(on bool) error {
	if f.loginErr != nil {
		return f.loginErr
	}
	f.login = on
	return nil
}

func (f *fakeHotkeys) HotkeySpecs() [2]string { return f.specs }
func (f *fakeHotkeys) BeginRecording()        { f.suspended = true }
func (f *fakeHotkeys) CancelRecording()       { f.suspended = false }
func (f *fakeHotkeys) SetHotkey(i int, spec string, done func(string)) {
	f.suspended = false
	f.sets++
	if spec == f.taken {
		done("快捷键 " + spec + " 已被其他程序占用")
		return
	}
	f.specs[i] = spec
	done("")
}
func (f *fakeHotkeys) RestoreDefaults(done func([2]string)) {
	f.suspended = false
	f.specs = [2]string{"Option+F1", "Option+F2"}
	done([2]string{})
}

type settingsRig struct {
	t    *testing.T
	host *fakeHotkeys
	w    *settingsWindow
	d    *app.Driver
}

func newSettingsRig(t *testing.T, goos string, rightAlt bool) *settingsRig {
	t.Helper()
	host := &fakeHotkeys{specs: [2]string{"Option+F1", "Option+F2"}}
	w := newSettingsWindow(host, sui.Light(), goos, func() bool { return rightAlt }, [2]string{})
	d, err := app.NewDriver(w.config(), new(vui.Canvas))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return &settingsRig{t: t, host: host, w: w, d: d}
}

func (r *settingsRig) click(id vui.ID) {
	r.t.Helper()
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == id {
			if err := r.d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
				r.t.Fatal(err)
			}
			return
		}
	}
	r.t.Fatalf("no control %s", sui.Tree.Text(id))
}

func (r *settingsRig) text(want string) bool {
	for _, l := range r.d.Snapshot().Texts {
		if strings.Contains(l.Value, want) {
			return true
		}
	}
	return false
}

func TestSettingsShowsHotkeys(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	for _, want := range []string{"截图", "剪切板历史", "⌥F1", "⌥F2", "恢复默认"} {
		if !r.text(want) {
			t.Errorf("settings window lacks %q", want)
		}
	}
	if r.text("长截图") || r.text("⌥F3") {
		t.Error("the long shot is gone")
	}
}

func TestSettingsRecordsFunctionKey(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[0])
	if !r.host.suspended || !r.text("请按下快捷键") {
		t.Fatal("recording should suspend the hotkeys and say so")
	}
	if err := r.d.Key(vui.KeyF5, vui.ModifierAlt|vui.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if r.host.specs[0] != "Option+Shift+F5" || r.host.suspended {
		t.Fatalf("specs = %v, suspended = %v", r.host.specs, r.host.suspended)
	}
	if !r.text("⌥⇧F5") || !r.text("已改为") {
		t.Fatal("new hotkey not shown")
	}
}

func TestSettingsRecordsLetterChord(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[1])
	if _, err := r.d.ShortcutChord(vui.ModifierMeta|vui.ModifierShift, 'v'); err != nil {
		t.Fatal(err)
	}
	if r.host.specs[1] != "CmdOrCtrl+Shift+V" {
		t.Fatalf("specs = %v", r.host.specs)
	}
}

func TestSettingsEscapeCancels(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[1])
	if err := r.d.Key(vui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if r.host.suspended || r.w.recording >= 0 || r.host.sets != 0 {
		t.Fatal("Escape should cancel without changing anything")
	}
}

func TestSettingsRejectsDuplicatesAndBareLetters(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[0])
	if err := r.d.Key(vui.KeyF2, vui.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if r.host.sets != 0 || !r.text("相同") || r.w.recording != 0 {
		t.Fatal("a hotkey another row uses must be refused, recording goes on")
	}
	if _, err := r.d.ShortcutChord(vui.ModifierShift|vui.ModifierMeta, '!'); err != nil {
		t.Fatal(err)
	}
	if r.host.sets != 0 {
		t.Fatal("punctuation is not a hotkey key")
	}
}

func TestSettingsTakenKeepsOld(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.host.taken = "Option+F9"
	r.click(settingsRows[0])
	if err := r.d.Key(vui.KeyF9, vui.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if r.host.specs[0] != "Option+F1" || !r.text("已保留原来的快捷键") || !r.text("未生效") {
		t.Fatalf("taken hotkey: specs %v", r.host.specs)
	}
}

func TestSettingsRestoreDefaults(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.host.specs[1] = "CmdOrCtrl+Shift+V"
	r.click(settingsDefaults)
	if r.host.specs[1] != "Option+F2" || !r.text("已恢复默认快捷键") {
		t.Fatalf("specs = %v", r.host.specs)
	}
}

func TestSettingsClosingWhileRecordingResumes(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[0])
	r.w.config().OnClosed()
	if r.host.suspended {
		t.Fatal("closing must give the hotkeys back")
	}
}

func TestRecordedHotkeyWindows(t *testing.T) {
	cases := []struct {
		mods     vui.Modifiers
		rightAlt bool
		want     platform.Hotkey
		ok       bool
	}{
		{vui.ModifierAlt, true, platform.Hotkey{Mods: platform.ModRightAlt, Key: platform.KeyF1}, true},
		{vui.ModifierAlt | vui.ModifierControl, true, platform.Hotkey{Mods: platform.ModRightAlt, Key: platform.KeyF1}, true}, // AltGr
		{vui.ModifierAlt, false, platform.Hotkey{Mods: platform.ModAlt, Key: platform.KeyF1}, true},
		{vui.ModifierControl | vui.ModifierShift, false, platform.Hotkey{Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: platform.KeyF1}, true},
		{vui.ModifierMeta, false, platform.Hotkey{}, false},
	}
	for _, c := range cases {
		got, ok := recordedHotkey("windows", platform.KeyF1, c.mods, c.rightAlt)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("recordedHotkey(%v, rightAlt=%v) = %+v,%v want %+v,%v", c.mods, c.rightAlt, got, ok, c.want, c.ok)
		}
	}
}
