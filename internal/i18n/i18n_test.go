package i18n

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		setting string
		system  bool
		want    Lang
	}{
		{SettingSystem, true, Chinese},
		{SettingSystem, false, English},
		{SettingChinese, false, Chinese},
		{SettingEnglish, true, English},
		{"fr", true, Chinese}, // an unknown setting follows the system
	}
	for _, c := range cases {
		if got := Resolve(c.setting, c.system); got != c.want {
			t.Errorf("Resolve(%q, %v) = %v, want %v", c.setting, c.system, got, c.want)
		}
	}
}

func TestT(t *testing.T) {
	defer Set(Current())
	Set(English)
	if T("截图", "Capture") != "Capture" || AppName() != "SnapLite" {
		t.Fatal("English")
	}
	Set(Chinese)
	if T("截图", "Capture") != "截图" || AppName() != "简截" {
		t.Fatal("Chinese")
	}
}
