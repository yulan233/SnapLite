package shell

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/history"
	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// hotkeyNames are the two hotkeys in config.HotkeySpecs order.
var hotkeyNames = [2]string{"截图", "剪切板历史"}

var (
	settingsFeature  = sui.Tree.Feature("settings")
	settingsRows     = [2]vui.ID{sui.Tree.ID(settingsFeature, "capture"), sui.Tree.ID(settingsFeature, "history")}
	settingsDefaults = sui.Tree.ID(settingsFeature, "defaults")
	settingsDone     = sui.Tree.ID(settingsFeature, "done")
	settingsTabs     = sui.Tree.Sub(settingsFeature, sui.Tree.Name("tabs"))

	settingsRecord      = sui.Tree.ID(settingsFeature, "record")
	settingsRecordText  = sui.Tree.ID(settingsFeature, "record-text")
	settingsMaxItems    = sui.Tree.Sub(settingsFeature, sui.Tree.Name("max-items"))
	settingsMaxBytes    = sui.Tree.Sub(settingsFeature, sui.Tree.Name("max-bytes"))
	settingsClick       = sui.Tree.Sub(settingsFeature, sui.Tree.Name("click"))
	settingsGrantAccess = sui.Tree.ID(settingsFeature, "grant-accessibility")
	settingsLogin       = sui.Tree.ID(settingsFeature, "login")
	settingsOpenDir     = sui.Tree.ID(settingsFeature, "open-dir")
	settingsQuality     = sui.Tree.Sub(settingsFeature, sui.Tree.Name("quality"))
	settingsScale       = sui.Tree.Sub(settingsFeature, sui.Tree.Name("scale"))
)

// Settings tabs.
const (
	tabHotkeys = iota
	tabCapture
	tabClipboard
	tabGeneral
)

var settingsTabLabels = []string{"快捷键", "截图", "剪切板", "通用"}

// qualityLabels name config.Qualities in the settings window.
var qualityLabels = []string{"原图（PNG 无损）", "高（JPEG 92%）", "中（JPEG 80%）", "低（JPEG 60%）"}

// QualityNote says what the quality setting does and does not change.
const QualityNote = "质量只影响保存的文件；复制到剪切板始终是无损 PNG。"

// settingsHost is what the settings window asks of the shell. Every call runs
// on the event-loop thread, and so do the done callbacks, possibly a loop turn later.
type settingsHost interface {
	HotkeySpecs() [2]string
	// BeginRecording takes the global hotkeys away, so the window receives
	// the chord being recorded; CancelRecording puts them back unchanged.
	BeginRecording()
	CancelRecording()
	// SetHotkey stores and registers one hotkey; done gets "" or why it failed
	// (the old hotkey is kept then).
	SetHotkey(index int, spec string, done func(problem string))
	RestoreDefaults(done func(problems [2]string))

	// Config is the live configuration the other tabs edit in place;
	// ApplyHistory puts the history fields into effect (limits, watching) and
	// SaveConfig writes the file (the capture fields take effect from it).
	Config() *config.Config
	ApplyHistory()
	SaveConfig()
	LaunchAtLogin() bool
	SetLaunchAtLogin(on bool) error
	// AccessibilityGranted says whether pasting into other applications can
	// work (macOS asks for the Accessibility permission).
	AccessibilityGranted() bool
	RequestAccessibility()
	OpenSaveDirectory()
}

// settingsWindow changes the hotkeys, the clipboard history and the general
// options, one tab each.
type settingsWindow struct {
	host     settingsHost
	tokens   sui.Tokens
	goos     string
	rightAlt func() bool // whether the right Alt key is down (Windows)
	close    func()

	view        flex.View
	wake        app.Invalidator
	tab         int
	itemsOpen   flex.SelectState
	bytesOpen   flex.SelectState
	qualityOpen flex.SelectState
	recording   int // the row being recorded, -1 for none
	problems    [2]string
	message     string
	failed      bool // message is a problem
	window      *app.Window
}

func newSettingsWindow(host settingsHost, tokens sui.Tokens, goos string, rightAlt func() bool, problems [2]string) *settingsWindow {
	return &settingsWindow{host: host, tokens: tokens, goos: goos, rightAlt: rightAlt, recording: -1, problems: problems}
}

func (w *settingsWindow) config() app.Config {
	return app.Config{
		Title: "简截 · 设置", Width: 480, Height: 460, Tree: &sui.Tree,
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
	w.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
		f.Column().Fill().Pad(24).Gap(16).Children(func() {
			if i, ok := f.Tabs(settingsTabs, settingsTabLabels, w.tab).TabClicked(); ok && i != w.tab {
				canvas.AfterPaint(func() { w.switchTab(i) })
			}
			f.Column().Grow(1).Gap(14).Children(func() {
				switch w.tab {
				case tabHotkeys:
					w.paintHotkeys(f, canvas)
				case tabCapture:
					w.paintCapture(f, canvas)
				case tabClipboard:
					w.paintClipboard(f, canvas)
				case tabGeneral:
					w.paintGeneral(f, canvas)
				}
			})
			f.Row().Gap(8).Justify(flex.End).Children(func() {
				if w.tab == tabHotkeys && f.Button(settingsDefaults, "恢复默认").Outline().Clicked() {
					canvas.AfterPaint(w.restoreDefaults)
				}
				if f.Button(settingsDone, "完成").Clicked() && w.close != nil {
					canvas.AfterPaint(w.close)
				}
			})
		})
	})
}

// switchTab leaves a recording unfinished when the hotkeys tab is left.
func (w *settingsWindow) switchTab(i int) {
	if w.recording >= 0 {
		w.toggle(w.recording)
	}
	w.tab = i
	w.say("", false)
}

// settingRow is one labelled line: the name on the leading side, the control
// after a spacer.
func settingRow(f *flex.Frame, label string, control func()) {
	f.Row().Gap(12).Items(flex.Center).Children(func() {
		f.Text(label)
		f.Spacer()
		control()
	})
}

func (w *settingsWindow) paintHotkeys(f *flex.Frame, canvas *vui.Canvas) {
	t := w.tokens
	specs := w.host.HotkeySpecs()
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
}

// choiceIndex is the index of v in choices, -1 when it is not one of them.
func choiceIndex[T comparable](choices []T, v T) int {
	for i, c := range choices {
		if c == v {
			return i
		}
	}
	return -1
}

// bytesLabel is "500 MB" or "1 GB".
func bytesLabel(n int64) string {
	if n >= 1<<30 && n%(1<<30) == 0 {
		return fmt.Sprintf("%d GB", n>>30)
	}
	return fmt.Sprintf("%d MB", n>>20)
}

func (w *settingsWindow) paintClipboard(f *flex.Frame, canvas *vui.Canvas) {
	cfg := w.host.Config()
	h := &cfg.History
	settingRow(f, "记录剪切板历史", func() {
		if f.Switch(settingsRecord, h.Enabled).Label("记录剪切板历史").Clicked() {
			canvas.AfterPaint(func() { h.Enabled = !h.Enabled; w.host.ApplyHistory() })
		}
	})
	settingRow(f, "记录复制的文字", func() {
		if f.Switch(settingsRecordText, h.RecordText).Label("记录复制的文字").Disabled(!h.Enabled).Clicked() {
			canvas.AfterPaint(func() { h.RecordText = !h.RecordText; w.host.ApplyHistory() })
		}
	})
	settingRow(f, "最多保存", func() {
		labels := make([]string, len(config.MaxItemsChoices))
		for i, n := range config.MaxItemsChoices {
			labels[i] = fmt.Sprintf("%d 条", n)
		}
		sel := f.Select(settingsMaxItems, &w.itemsOpen, labels, choiceIndex(config.MaxItemsChoices, h.MaxItems)).
			Placeholder(fmt.Sprintf("%d 条", h.MaxItems)).Width(140)
		if i, ok := sel.Picked(); ok {
			canvas.AfterPaint(func() { h.MaxItems = config.MaxItemsChoices[i]; w.host.ApplyHistory() })
		}
	})
	settingRow(f, "图片最多占用", func() {
		labels := make([]string, len(config.MaxBytesChoices))
		for i, n := range config.MaxBytesChoices {
			labels[i] = bytesLabel(n)
		}
		sel := f.Select(settingsMaxBytes, &w.bytesOpen, labels, choiceIndex(config.MaxBytesChoices, h.MaxBytes)).
			Placeholder(bytesLabel(h.MaxBytes)).Width(140)
		if i, ok := sel.Picked(); ok {
			canvas.AfterPaint(func() { h.MaxBytes = config.MaxBytesChoices[i]; w.host.ApplyHistory() })
		}
	})
	settingRow(f, "点击条目时", func() {
		current := 0
		if h.ClickAction == config.ClickPaste {
			current = 1
		}
		if i, ok := f.Choice(settingsClick, []string{"仅复制", "复制并粘贴"}, current).ChosenIndex(); ok && i != current {
			canvas.AfterPaint(func() {
				h.ClickAction = config.ClickCopy
				if i == 1 {
					h.ClickAction = config.ClickPaste
				}
				w.host.SaveConfig()
			})
		}
	})
	if !w.host.AccessibilityGranted() {
		f.Row().Gap(12).Items(flex.Center).Children(func() {
			f.Wrap("粘贴到其他应用需要在 系统设置 › 隐私与安全性 › 辅助功能 中允许简截。").Muted().Grow(1)
			if f.Button(settingsGrantAccess, "去开启").Outline().Small().Clicked() {
				canvas.AfterPaint(w.host.RequestAccessibility)
			}
		})
	}
	f.Wrap("相同的内容只保留一条：再次复制或从历史里恢复时，它会移到最前面。").Muted()
}

func (w *settingsWindow) paintCapture(f *flex.Frame, canvas *vui.Canvas) {
	cfg := w.host.Config()
	c := &cfg.Capture
	settingRow(f, "图片质量", func() {
		current := max(choiceIndex(config.Qualities, c.Quality), 0)
		sel := f.Select(settingsQuality, &w.qualityOpen, qualityLabels, current).Width(180)
		if i, ok := sel.Picked(); ok {
			canvas.AfterPaint(func() { c.Quality = config.Qualities[i]; w.host.SaveConfig() })
		}
	})
	settingRow(f, "分辨率", func() {
		current := 0
		if c.Scale == config.Scale1x {
			current = 1
		}
		if i, ok := f.Choice(settingsScale, []string{"原始", "标准 1×"}, current).ChosenIndex(); ok && i != current {
			canvas.AfterPaint(func() {
				c.Scale = config.ScaleOriginal
				if i == 1 {
					c.Scale = config.Scale1x
				}
				w.host.SaveConfig()
			})
		}
	})
	settingRow(f, "截图保存目录", func() {
		if f.Button(settingsOpenDir, "打开").Outline().Small().Clicked() {
			canvas.AfterPaint(w.host.OpenSaveDirectory)
		}
	})
	f.Text(cfg.SaveDirectory()).Muted().FontSize(sui.MutedTextSize)
	f.Wrap(QualityNote).Muted()
	f.Wrap("「标准 1×」把高分屏截图缩小到屏幕的逻辑尺寸，文件更小；复制、保存、剪切板历史和钉图都用缩小后的图片。").Muted()
}

func (w *settingsWindow) paintGeneral(f *flex.Frame, canvas *vui.Canvas) {
	settingRow(f, "开机时启动简截", func() {
		on := w.host.LaunchAtLogin()
		if f.Switch(settingsLogin, on).Label("开机时启动简截").Clicked() {
			canvas.AfterPaint(func() {
				if err := w.host.SetLaunchAtLogin(!on); err != nil {
					w.say("开机启动设置失败：直接运行的程序不支持，请从「简截.app」启动。", true)
				} else {
					w.say("", false)
				}
			})
		}
	})
	if w.message != "" && w.failed {
		f.Wrap(w.message).Color(w.tokens.Danger)
	}
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
	w.host.RestoreDefaults(func(problems [2]string) {
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

func (s *Shell) HotkeySpecs() [2]string {
	p := s.cfg.HotkeySpecs()
	return [2]string{*p[0], *p[1]}
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

func (s *Shell) RestoreDefaults(done func(problems [2]string)) {
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

func (s *Shell) Config() *config.Config { return s.cfg }

func (s *Shell) SaveConfig() {
	if err := s.cfg.Save(); err != nil {
		s.log.Printf("config: %v", err)
	}
}

// ApplyHistory puts the history settings into effect at once: the limits
// (dropping the oldest items beyond them) and whether the clipboard is watched.
func (s *Shell) ApplyHistory() {
	if store := s.extra.history; store != nil {
		store.SetLimits(history.Limits{MaxItems: s.cfg.History.MaxItems, MaxBytes: s.cfg.History.MaxBytes})
	}
	s.setWatching(s.cfg.History.Enabled)
	s.SaveConfig()
}

func (s *Shell) LaunchAtLogin() bool            { return platform.LaunchAtLogin() }
func (s *Shell) SetLaunchAtLogin(on bool) error { return platform.SetLaunchAtLogin(on) }
func (s *Shell) AccessibilityGranted() bool     { return platform.HasAccessibilityPermission() }

func (s *Shell) RequestAccessibility() {
	platform.RequestAccessibilityPermission()
	platform.OpenPrivacySettings("accessibility")
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
