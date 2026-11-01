package shell

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// hotkeyNames are the three hotkeys in config.HotkeySpecs order.
var hotkeyNames = [3]string{"截图", "长截图", "剪切板历史"}

var (
	settingsFeature  = sui.Tree.Feature("settings")
	settingsRows     = [3]vui.ID{sui.Tree.ID(settingsFeature, "capture"), sui.Tree.ID(settingsFeature, "longshot"), sui.Tree.ID(settingsFeature, "history")}
	settingsDefaults = sui.Tree.ID(settingsFeature, "defaults")
	settingsDone     = sui.Tree.ID(settingsFeature, "done")
)

// hotkeyHost is what the settings window asks of the shell. Every call runs on
// the event-loop thread, and so do the done callbacks, possibly a loop turn later.
type hotkeyHost interface {
	HotkeySpecs() [3]string
	// BeginRecording takes the global hotkeys away, so the window receives
	// the chord being recorded; CancelRecording puts them back unchanged.
	BeginRecording()
	CancelRecording()
	// SetHotkey stores and registers one hotkey; done gets "" or why it failed
	// (the old hotkey is kept then).
	SetHotkey(index int, spec string, done func(problem string))
	RestoreDefaults(done func(problems [3]string))
}

// settingsWindow records and changes the global hotkeys.
type settingsWindow struct {
	host     hotkeyHost
	tokens   sui.Tokens
	goos     string
	rightAlt func() bool // whether the right Alt key is down (Windows)
	close    func()

	view      flex.View
	wake      app.Invalidator
	recording int // the row being recorded, -1 for none
	problems  [3]string
	message   string
	failed    bool // message is a problem
	window    *app.Window
}

func newSettingsWindow(host hotkeyHost, tokens sui.Tokens, goos string, rightAlt func() bool, problems [3]string) *settingsWindow {
	return &settingsWindow{host: host, tokens: tokens, goos: goos, rightAlt: rightAlt, recording: -1, problems: problems}
}

func (w *settingsWindow) config() app.Config {
	return app.Config{
		Title: "简截 · 设置", Width: 460, Height: 400, Tree: &sui.Tree,
		Paint:       w.paint,
		OnKey:       w.key,
		OnShortcut:  w.shortcut,
		Invalidator: &w.wake,
		OnClosed: func() {
			if w.recording >= 0 {
				w.recording = -1
				w.host.CancelRecording()
			}
		},
	}
}

// settingsHint is the platform's note under the hotkey rows.
func settingsHint(goos string) string {
	if goos == "darwin" {
		return "点击快捷键后按下新的组合，Esc 取消。Mac 键盘上的 F1–F12 默认是亮度、音量等功能键，录制和使用时要同时按住 fn，或在 系统设置 › 键盘 中设为标准功能键。"
	}
	return "点击快捷键后按下新的组合，Esc 取消。Alt 组合会区分左右 Alt。"
}

// display is what row i's button says.
func (w *settingsWindow) display(i int, spec string) string {
	if w.recording == i {
		return "请按下快捷键…"
	}
	if text := hotkeyText(spec); text != "" {
		return text
	}
	return spec + "（无效）"
}

func (w *settingsWindow) paint(canvas *vui.Canvas, size vui.Size) {
	t := w.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, t.Surface)
	specs := w.host.HotkeySpecs()
	w.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
		f.Column().Fill().Pad(24).Gap(14).Children(func() {
			f.Text("快捷键").Bold()
			for i := range hotkeyNames {
				f.Row().Gap(12).Items(flex.Center).Children(func() {
					f.Text(hotkeyNames[i]).Width(96)
					button := f.Button(settingsRows[i], w.display(i, specs[i])).Width(200)
					if w.recording != i {
						button = button.Outline()
					}
					if button.Clicked() {
						canvas.AfterPaint(func() { w.toggle(i) })
					}
					if w.problems[i] != "" && w.recording != i {
						f.Text("未生效").Color(t.Danger)
					}
				})
			}
			f.Wrap(settingsHint(w.goos)).Muted()
			if w.message != "" {
				if w.failed {
					f.Wrap(w.message).Color(t.Danger)
				} else {
					f.Wrap(w.message).Muted()
				}
			}
			f.Row().Grow(1)
			f.Row().Gap(8).Justify(flex.End).Children(func() {
				if f.Button(settingsDefaults, "恢复默认").Outline().Clicked() {
					canvas.AfterPaint(w.restoreDefaults)
				}
				if f.Button(settingsDone, "完成").Clicked() && w.close != nil {
					canvas.AfterPaint(w.close)
				}
			})
		})
	})
}

func (w *settingsWindow) say(message string, failed bool) {
	w.message, w.failed = message, failed
}

// toggle starts recording row i, switches recording to it, or stops.
func (w *settingsWindow) toggle(i int) {
	switch {
	case w.recording == i:
		w.recording = -1
		w.say("", false)
		w.host.CancelRecording()
		return
	case w.recording < 0:
		w.host.BeginRecording()
	}
	w.recording = i
	w.say("请按下新的快捷键，按 Esc 取消。", false)
}

var settingsHandled = vui.EventResult{Handled: true, Repaint: true}

func (w *settingsWindow) key(e vui.KeyEvent) vui.EventResult {
	if w.recording < 0 || e.Release {
		return vui.EventResult{}
	}
	switch {
	case e.Key == vui.KeyEscape && e.Modifiers == 0:
		w.toggle(w.recording)
	case e.Key >= vui.KeyF1 && e.Key <= vui.KeyF12:
		w.record(platform.KeyF1+rune(e.Key-vui.KeyF1), e.Modifiers)
	}
	// Every other key is swallowed while recording.
	return settingsHandled
}

func (w *settingsWindow) shortcut(e vui.KeyEvent) vui.EventResult {
	if w.recording < 0 {
		return vui.EventResult{}
	}
	r := []rune(strings.ToUpper(e.Text))
	if len(r) == 1 && (r[0] >= 'A' && r[0] <= 'Z' || r[0] >= '0' && r[0] <= '9') {
		w.record(r[0], e.Modifiers)
	}
	return settingsHandled
}

// recordedHotkey turns a key and Viva's modifiers into a hotkey: Command is
// CmdOrCtrl on macOS, Control is CmdOrCtrl on Windows, where right Alt is told
// from left Alt by rightAlt and the Windows key cannot be used.
func recordedHotkey(goos string, key rune, mods vui.Modifiers, rightAlt bool) (platform.Hotkey, bool) {
	h := platform.Hotkey{Key: key}
	if mods&vui.ModifierShift != 0 {
		h.Mods |= platform.ModShift
	}
	if goos == "darwin" {
		if mods&vui.ModifierMeta != 0 {
			h.Mods |= platform.ModCmdOrCtrl
		}
		if mods&vui.ModifierControl != 0 {
			h.Mods |= platform.ModCtrl
		}
		if mods&vui.ModifierAlt != 0 {
			h.Mods |= platform.ModAlt
		}
		return h, true
	}
	if mods&vui.ModifierMeta != 0 {
		return h, false
	}
	switch {
	case mods&vui.ModifierAlt != 0 && rightAlt:
		// AltGr layouts report right Alt as Control+Alt.
		h.Mods |= platform.ModRightAlt
	case mods&vui.ModifierAlt != 0:
		h.Mods |= platform.ModAlt
		if mods&vui.ModifierControl != 0 {
			h.Mods |= platform.ModCmdOrCtrl
		}
	case mods&vui.ModifierControl != 0:
		h.Mods |= platform.ModCmdOrCtrl
	}
	return h, true
}

func sameHotkey(a, b string) bool {
	ha, errA := config.ParseHotkey(a)
	hb, errB := config.ParseHotkey(b)
	return errA == nil && errB == nil && ha == hb
}

func (w *settingsWindow) record(key rune, mods vui.Modifiers) {
	i := w.recording
	h, ok := recordedHotkey(w.goos, key, mods, w.rightAlt())
	if !ok {
		w.say("不支持 Windows 徽标键组合，请换一个。", true)
		return
	}
	spec := config.SpecFor(w.goos, h)
	if _, err := config.ParseHotkey(spec); err != nil {
		w.say("字母和数字要搭配 ⌘、⌃ 或 ⌥（Windows 为 Ctrl 或 Alt），或者改用 F1–F12。", true)
		return
	}
	for j, other := range w.host.HotkeySpecs() {
		if j != i && sameHotkey(other, spec) {
			w.say(fmt.Sprintf("和「%s」的快捷键相同，请换一个。", hotkeyNames[j]), true)
			return
		}
	}
	w.recording = -1
	w.say("正在应用…", false)
	w.host.SetHotkey(i, spec, func(problem string) {
		w.problems[i] = problem
		if problem != "" {
			w.say(problem+"，已保留原来的快捷键。", true)
		} else {
			w.say(fmt.Sprintf("「%s」已改为 %s。", hotkeyNames[i], config.FormatHotkeyFor(w.goos, h)), false)
		}
		w.wake.Invalidate()
	})
}

func (w *settingsWindow) restoreDefaults() {
	w.recording = -1
	w.say("正在应用…", false)
	w.host.RestoreDefaults(func(problems [3]string) {
		w.problems = problems
		w.say("已恢复默认快捷键。", false)
		for _, p := range problems {
			if p != "" {
				w.say(p+"。", true)
				break
			}
		}
		w.wake.Invalidate()
	})
}

// ---- the shell side ---------------------------------------------------------

func (s *Shell) HotkeySpecs() [3]string {
	p := s.cfg.HotkeySpecs()
	return [3]string{*p[0], *p[1], *p[2]}
}

func (s *Shell) BeginRecording() { s.suspendHotkeys() }

// CancelRecording registers the hotkeys again on a later turn, after the
// unregistering suspendHotkeys posted.
func (s *Shell) CancelRecording() {
	platform.PostMain(func() { s.registerHotkeys() })
}

func (s *Shell) SetHotkey(index int, spec string, done func(problem string)) {
	slot := s.cfg.HotkeySpecs()[index]
	old := *slot
	*slot = spec
	s.suspendHotkeys()
	platform.PostMain(func() {
		defer s.guard("hotkey")
		if problem := s.registerHotkeys()[index]; problem != "" {
			// The new one is not registered, so the old one can be at once.
			*slot = old
			s.registerHotkeys()
			done(problem)
			return
		}
		if err := s.cfg.Save(); err != nil {
			s.log.Printf("config: %v", err)
			done("快捷键已生效，但写入 config.json 失败")
			return
		}
		done("")
	})
}

func (s *Shell) RestoreDefaults(done func(problems [3]string)) {
	s.suspendHotkeys()
	defaults := config.Default().HotkeySpecs()
	for i, slot := range s.cfg.HotkeySpecs() {
		*slot = *defaults[i]
	}
	platform.PostMain(func() {
		defer s.guard("hotkey")
		problems := s.registerHotkeys()
		if err := s.cfg.Save(); err != nil {
			s.log.Printf("config: %v", err)
		}
		done(problems)
	})
}

// ShowSettings opens the settings window, or brings it forward.
func (s *Shell) ShowSettings() {
	defer s.guard("settings")
	if s.settings != nil && s.settings.window != nil && !s.settings.window.Closed() {
		s.settings.window.Show()
		platform.ActivateApp()
		return
	}
	w := newSettingsWindow(s, sui.Current(), runtime.GOOS, platform.RightAltDown, s.hotkeyProblems)
	config := w.config()
	closed := config.OnClosed
	config.OnClosed = func() {
		closed()
		s.settings = nil
	}
	window, err := s.app.OpenWindow(config, new(vui.Canvas))
	if err != nil {
		s.log.Printf("settings window: %v", err)
		return
	}
	w.window, w.close = window, window.Close
	s.settings = w
	platform.ActivateApp()
}
