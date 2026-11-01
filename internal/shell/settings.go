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
	"snaplite/internal/i18n"
	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

// hotkeyName is one of the two hotkeys, in config.HotkeySpecs order.
func hotkeyName(i int) string {
	if i == 0 {
		return i18n.T("截图", "Capture")
	}
	return i18n.T("剪切板历史", "Clipboard history")
}

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
	settingsLanguage    = sui.Tree.Sub(settingsFeature, sui.Tree.Name("language"))
)

// Settings tabs.
const (
	tabHotkeys = iota
	tabCapture
	tabClipboard
	tabGeneral
)

func settingsTabLabels() []string {
	return []string{i18n.T("快捷键", "Hotkeys"), i18n.T("截图", "Capture"), i18n.T("剪切板", "Clipboard"), i18n.T("通用", "General")}
}

// qualityLabels name config.Qualities in the settings window.
func qualityLabels() []string {
	return []string{
		i18n.T("原图（PNG 无损）", "Original (lossless PNG)"), i18n.T("高（JPEG 92%）", "High (JPEG 92%)"),
		i18n.T("中（JPEG 80%）", "Medium (JPEG 80%)"), i18n.T("低（JPEG 60%）", "Low (JPEG 60%)"),
	}
}

// languageLabels name i18n.Settings. Each language is shown in itself.
func languageLabels() []string {
	return []string{i18n.T("跟随系统", "System"), "中文", "English"}
}

// QualityNote says what the quality setting does and does not change.
func QualityNote() string {
	return i18n.T("质量只影响保存的文件；复制到剪切板始终是无损 PNG。",
		"Quality only affects saved files; the clipboard always gets lossless PNG.")
}

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
	// SetLanguage switches the interface language and saves it.
	SetLanguage(setting string)
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
	langOpen    flex.SelectState
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
		Title: i18n.T("简截 · 设置", "SnapLite · Settings"), Width: 480, Height: 460, Tree: &sui.Tree,
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
		return i18n.T("点击快捷键后按下新的组合，Esc 取消。Mac 键盘上的 F1–F12 默认是亮度、音量等功能键，录制和使用时要同时按住 fn，或在 系统设置 › 键盘 中设为标准功能键。",
			"Click a hotkey, then press the new combination; Esc cancels. On a Mac keyboard F1–F12 control brightness, volume and so on by default: hold fn while recording and using them, or make them standard function keys in System Settings › Keyboard.")
	}
	return i18n.T("点击快捷键后按下新的组合，Esc 取消。Alt 组合会区分左右 Alt。",
		"Click a hotkey, then press the new combination; Esc cancels. Alt combinations tell the left and right Alt keys apart.")
}

// display is what row i's button says.
func (w *settingsWindow) display(i int, spec string) string {
	if w.recording == i {
		return i18n.T("请按下快捷键…", "Press a hotkey…")
	}
	if text := hotkeyText(spec); text != "" {
		return text
	}
	return spec + i18n.T("（无效）", " (invalid)")
}

func (w *settingsWindow) paint(canvas *vui.Canvas, size vui.Size) {
	t := w.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, t.Surface)
	w.view.Draw(canvas, t.VivaTheme(), bounds, func(f *flex.Frame) {
		f.Column().Fill().Pad(24).Gap(16).Children(func() {
			if i, ok := f.Tabs(settingsTabs, settingsTabLabels(), w.tab).TabClicked(); ok && i != w.tab {
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
				if w.tab == tabHotkeys && f.Button(settingsDefaults, i18n.T("恢复默认", "Restore Defaults")).Outline().Clicked() {
					canvas.AfterPaint(w.restoreDefaults)
				}
				if f.Button(settingsDone, i18n.T("完成", "Done")).Clicked() && w.close != nil {
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
	for i := range specs {
		f.Row().Gap(12).Items(flex.Center).Children(func() {
			f.Text(hotkeyName(i)).Width(120)
			button := f.Button(settingsRows[i], w.display(i, specs[i])).Width(200)
			if w.recording != i {
				button = button.Outline()
			}
			if button.Clicked() {
				canvas.AfterPaint(func() { w.toggle(i) })
			}
			if w.problems[i] != "" && w.recording != i {
				f.Text(i18n.T("未生效", "Not active")).Color(t.Danger)
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
	record := i18n.T("记录剪切板历史", "Record clipboard history")
	settingRow(f, record, func() {
		if f.Switch(settingsRecord, h.Enabled).Label(record).Clicked() {
			canvas.AfterPaint(func() { h.Enabled = !h.Enabled; w.host.ApplyHistory() })
		}
	})
	recordText := i18n.T("记录复制的文字", "Record copied text")
	settingRow(f, recordText, func() {
		if f.Switch(settingsRecordText, h.RecordText).Label(recordText).Disabled(!h.Enabled).Clicked() {
			canvas.AfterPaint(func() { h.RecordText = !h.RecordText; w.host.ApplyHistory() })
		}
	})
	settingRow(f, i18n.T("最多保存", "Keep at most"), func() {
		items := i18n.T("%d 条", "%d items")
		labels := make([]string, len(config.MaxItemsChoices))
		for i, n := range config.MaxItemsChoices {
			labels[i] = fmt.Sprintf(items, n)
		}
		sel := f.Select(settingsMaxItems, &w.itemsOpen, labels, choiceIndex(config.MaxItemsChoices, h.MaxItems)).
			Placeholder(fmt.Sprintf(items, h.MaxItems)).Width(140)
		if i, ok := sel.Picked(); ok {
			canvas.AfterPaint(func() { h.MaxItems = config.MaxItemsChoices[i]; w.host.ApplyHistory() })
		}
	})
	settingRow(f, i18n.T("图片最多占用", "Images use at most"), func() {
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
	settingRow(f, i18n.T("点击条目时", "Clicking an item"), func() {
		current := 0
		if h.ClickAction == config.ClickPaste {
			current = 1
		}
		if i, ok := f.Choice(settingsClick, []string{i18n.T("仅复制", "Copies"), i18n.T("复制并粘贴", "Copies and pastes")}, current).ChosenIndex(); ok && i != current {
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
			f.Wrap(i18n.T("粘贴到其他应用需要在 系统设置 › 隐私与安全性 › 辅助功能 中允许简截。",
				"Pasting into other apps needs SnapLite allowed in System Settings › Privacy & Security › Accessibility.")).Muted().Grow(1)
			if f.Button(settingsGrantAccess, i18n.T("去开启", "Allow")).Outline().Small().Clicked() {
				canvas.AfterPaint(w.host.RequestAccessibility)
			}
		})
	}
	f.Wrap(i18n.T("相同的内容只保留一条：再次复制或从历史里恢复时，它会移到最前面。",
		"The same content is kept only once: copying it again or restoring it from the history moves it to the top.")).Muted()
}

func (w *settingsWindow) paintCapture(f *flex.Frame, canvas *vui.Canvas) {
	cfg := w.host.Config()
	c := &cfg.Capture
	settingRow(f, i18n.T("图片质量", "Image quality"), func() {
		current := max(choiceIndex(config.Qualities, c.Quality), 0)
		sel := f.Select(settingsQuality, &w.qualityOpen, qualityLabels(), current).Width(200)
		if i, ok := sel.Picked(); ok {
			canvas.AfterPaint(func() { c.Quality = config.Qualities[i]; w.host.SaveConfig() })
		}
	})
	settingRow(f, i18n.T("分辨率", "Resolution"), func() {
		current := 0
		if c.Scale == config.Scale1x {
			current = 1
		}
		if i, ok := f.Choice(settingsScale, []string{i18n.T("原始", "Original"), i18n.T("标准 1×", "Standard 1×")}, current).ChosenIndex(); ok && i != current {
			canvas.AfterPaint(func() {
				c.Scale = config.ScaleOriginal
				if i == 1 {
					c.Scale = config.Scale1x
				}
				w.host.SaveConfig()
			})
		}
	})
	settingRow(f, i18n.T("截图保存目录", "Screenshots folder"), func() {
		if f.Button(settingsOpenDir, i18n.T("打开", "Open")).Outline().Small().Clicked() {
			canvas.AfterPaint(w.host.OpenSaveDirectory)
		}
	})
	f.Text(cfg.SaveDirectory()).Muted().FontSize(sui.MutedTextSize)
	f.Wrap(QualityNote()).Muted()
	f.Wrap(i18n.T("「标准 1×」把高分屏截图缩小到屏幕的逻辑尺寸，文件更小；复制、保存、剪切板历史和钉图都用缩小后的图片。",
		"“Standard 1×” shrinks high-DPI screenshots to the screen's logical size for smaller files; copying, saving, the clipboard history and pins all use the smaller picture.")).Muted()
}

func (w *settingsWindow) paintGeneral(f *flex.Frame, canvas *vui.Canvas) {
	settingRow(f, i18n.T("语言", "Language"), func() {
		current := max(choiceIndex(i18n.Settings, w.host.Config().Language), 0)
		sel := f.Select(settingsLanguage, &w.langOpen, languageLabels(), current).Width(160)
		if i, ok := sel.Picked(); ok && i != current {
			canvas.AfterPaint(func() { w.host.SetLanguage(i18n.Settings[i]) })
		}
	})
	login := i18n.T("开机时启动简截", "Launch SnapLite at login")
	settingRow(f, login, func() {
		on := w.host.LaunchAtLogin()
		if f.Switch(settingsLogin, on).Label(login).Clicked() {
			canvas.AfterPaint(func() {
				if err := w.host.SetLaunchAtLogin(!on); err != nil {
					w.say(i18n.T("开机启动设置失败：直接运行的程序不支持，请从「简截.app」启动。",
						"Could not set launch at login: a bare executable is not supported, start SnapLite from “简截.app”."), true)
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
	w.say(i18n.T("请按下新的快捷键，按 Esc 取消。", "Press the new hotkey, or Esc to cancel."), false)
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
		w.say(i18n.T("不支持 Windows 徽标键组合，请换一个。", "Windows logo key combinations are not supported; try another."), true)
		return
	}
	spec := config.SpecFor(w.goos, h)
	if _, err := config.ParseHotkey(spec); err != nil {
		w.say(i18n.T("字母和数字要搭配 ⌘、⌃ 或 ⌥（Windows 为 Ctrl 或 Alt），或者改用 F1–F12。",
			"Letters and digits need ⌘, ⌃ or ⌥ (Ctrl or Alt on Windows); or use F1–F12."), true)
		return
	}
	for j, other := range w.host.HotkeySpecs() {
		if j != i && sameHotkey(other, spec) {
			w.say(fmt.Sprintf(i18n.T("和「%s」的快捷键相同，请换一个。", "Same as the %s hotkey; try another."), hotkeyName(j)), true)
			return
		}
	}
	w.recording = -1
	w.say(i18n.T("正在应用…", "Applying…"), false)
	w.host.SetHotkey(i, spec, func(problem string) {
		w.problems[i] = problem
		if problem != "" {
			w.say(problem+i18n.T("，已保留原来的快捷键。", "; the previous hotkey is kept."), true)
		} else {
			w.say(fmt.Sprintf(i18n.T("「%s」已改为 %s。", "%s is now %s."), hotkeyName(i), config.FormatHotkeyFor(w.goos, h)), false)
		}
		w.wake.Invalidate()
	})
}

func (w *settingsWindow) restoreDefaults() {
	w.recording = -1
	w.say(i18n.T("正在应用…", "Applying…"), false)
	w.host.RestoreDefaults(func(problems [2]string) {
		w.problems = problems
		w.say(i18n.T("已恢复默认快捷键。", "Default hotkeys restored."), false)
		for _, p := range problems {
			if p != "" {
				w.say(p+i18n.T("。", "."), true)
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
			done(i18n.T("快捷键已生效，但写入 config.json 失败", "The hotkey works, but config.json could not be written"))
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

// SetLanguage switches the interface language. Open windows show it from
// their next paint and the tray menu the next time it opens.
func (s *Shell) SetLanguage(setting string) {
	s.cfg.Language = setting
	s.applyLanguage()
	// Hotkey problems are texts: registering again (which leaves the working
	// hotkeys alone) writes them, and the tray tooltip, in the new language.
	problems := s.registerHotkeys()
	if s.settings != nil {
		s.settings.problems = problems
	}
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
