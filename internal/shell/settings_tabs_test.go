package shell

import (
	"errors"
	"testing"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/config"
	"snaplite/internal/i18n"
)

func (r *settingsRig) openTab(i int) {
	r.t.Helper()
	r.click(settingsTabs.At(i))
	if r.w.tab != i {
		r.t.Fatalf("tab %d did not open", i)
	}
}

func TestSettingsClipboardTab(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.openTab(tabClipboard)
	h := &r.host.Config().History
	for _, want := range []string{"记录剪切板历史", "最多保存", "100 条", "500 MB", "仅复制", "复制并粘贴", "去开启"} {
		if !r.text(want) {
			t.Errorf("clipboard tab lacks %q", want)
		}
	}
	r.click(settingsRecord)
	if h.Enabled || r.host.applied != 1 {
		t.Fatalf("switching history off: enabled %v, applied %d", h.Enabled, r.host.applied)
	}
	r.click(settingsRecord)
	r.click(settingsMaxItems.At(len(config.MaxItemsChoices))) // the trigger opens the list
	r.click(settingsMaxItems.At(1))
	if h.MaxItems != 50 {
		t.Fatalf("max items = %d, want 50", h.MaxItems)
	}
	r.click(settingsClick.At(1))
	if h.ClickAction != config.ClickPaste || r.host.saved == 0 {
		t.Fatalf("click action = %q", h.ClickAction)
	}
	r.click(settingsGrantAccess)
	if r.host.requested != 1 {
		t.Fatal("去开启 asks for the permission")
	}
}

func TestSettingsGeneralTab(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.openTab(tabGeneral)
	r.click(settingsLogin)
	if !r.host.login {
		t.Fatal("launch at login switched on")
	}
	if r.text("长截图") || r.text("截图保存目录") {
		t.Fatal("the general tab keeps only launching at login")
	}
	r.host.loginErr = errors.New("not in a bundle")
	r.click(settingsLogin)
	if !r.text("开机启动设置失败") {
		t.Fatal("a failure is explained")
	}
}

func TestSettingsCaptureTab(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.openTab(tabCapture)
	c := &r.host.Config().Capture
	for _, want := range []string{"图片质量", "原图（PNG 无损）", "分辨率", "原始", "标准 1×", "截图保存目录", QualityNote()} {
		if !r.text(want) {
			t.Errorf("capture tab lacks %q", want)
		}
	}
	r.click(settingsQuality.At(len(config.Qualities))) // the trigger opens the list
	for _, want := range []string{"高（JPEG 92%）", "中（JPEG 80%）", "低（JPEG 60%）"} {
		if !r.text(want) {
			t.Errorf("quality list lacks %q", want)
		}
	}
	r.click(settingsQuality.At(2))
	if c.Quality != config.QualityMedium || r.host.saved != 1 {
		t.Fatalf("quality = %q, saved %d", c.Quality, r.host.saved)
	}
	if !r.text("中（JPEG 80%）") {
		t.Fatal("the select shows the new quality")
	}
	r.click(settingsScale.At(1))
	if c.Scale != config.Scale1x || r.host.saved != 2 {
		t.Fatalf("scale = %q, saved %d", c.Scale, r.host.saved)
	}
	r.click(settingsScale.At(0))
	if c.Scale != config.ScaleOriginal {
		t.Fatal("back to the original resolution")
	}
	r.click(settingsOpenDir)
	if r.host.openedDirs != 1 {
		t.Fatal("open save directory")
	}
}

func TestSettingsLeavingTabCancelsRecording(t *testing.T) {
	r := newSettingsRig(t, "darwin", false)
	r.click(settingsRows[0])
	r.openTab(tabGeneral)
	if r.w.recording >= 0 || r.host.suspended {
		t.Fatal("leaving the hotkeys tab gives the hotkeys back")
	}
}

func TestSettingsTabsFitWindow(t *testing.T) {
	defer i18n.Set(i18n.Current())
	for _, lang := range []i18n.Lang{i18n.Chinese, i18n.English} {
		i18n.Set(lang)
		testSettingsTabsFit(t, lang)
	}
}

func testSettingsTabsFit(t *testing.T, lang i18n.Lang) {
	for _, goos := range []string{"darwin", "windows"} {
		for tab := range settingsTabLabels() {
			r := newSettingsRig(t, goos, false)
			if tab != 0 {
				r.openTab(tab)
			} else {
				r.click(settingsRows[0])
				_ = r.d.Key(vui.KeyF2, vui.ModifierAlt) // a refusal adds a message line
			}
			snap := r.d.Snapshot()
			for _, h := range snap.Hits {
				if h.Bounds.Y+h.Bounds.Height > float32(snap.Height) || h.Bounds.X+h.Bounds.Width > float32(snap.Width) {
					t.Errorf("lang %d %s tab %d: %q at %+v spills out of %dx%d", lang, goos, tab, h.Label, h.Bounds, snap.Width, snap.Height)
				}
			}
			// Texts too, above the 完成 row: a note that runs under it is cut off.
			done, _ := r.hitBounds(settingsDone)
			for _, text := range snap.Texts {
				if text.Value != i18n.T("完成", "Done") && text.Value != i18n.T("恢复默认", "Restore Defaults") && text.Bounds.Y+text.Bounds.Height > done.Y {
					t.Errorf("lang %d %s tab %d: text %q at %+v runs under the buttons at %v", lang, goos, tab, text.Value, text.Bounds, done.Y)
				}
			}
		}
	}
}

func (r *settingsRig) hitBounds(id vui.ID) (vui.Rect, bool) {
	for _, h := range r.d.Snapshot().Hits {
		if h.ID == id {
			return h.Bounds, true
		}
	}
	return vui.Rect{}, false
}

func TestSettingsLanguageSwitch(t *testing.T) {
	defer i18n.Set(i18n.Current())
	i18n.Set(i18n.Chinese)
	r := newSettingsRig(t, "windows", false)
	r.openTab(tabGeneral)
	r.click(settingsLanguage.At(len(i18n.Settings))) // the trigger opens the list
	r.click(settingsLanguage.At(2))
	if got := r.host.Config().Language; got != i18n.SettingEnglish || i18n.Current() != i18n.English {
		t.Fatalf("language = %q, current %v", got, i18n.Current())
	}
	r.openTab(tabGeneral) // repaints
	if !r.text("Language") || !r.text("Launch SnapLite at login") || r.text("语言") {
		t.Fatal("the window is not in English after switching")
	}
}
