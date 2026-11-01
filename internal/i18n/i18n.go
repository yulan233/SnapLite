// Package i18n picks SnapLite's interface language: Chinese or English.
package i18n

import "sync/atomic"

// Lang is an interface language.
type Lang int32

const (
	Chinese Lang = iota
	English
)

// The language settings config.json stores.
const (
	SettingSystem  = "" // follow the system's preferred language
	SettingChinese = "zh"
	SettingEnglish = "en"
)

// Settings are the language settings in the order the settings window lists them.
var Settings = []string{SettingSystem, SettingChinese, SettingEnglish}

// current is read from every window's paint and written by the settings
// window, all on the event-loop thread; atomic so tests and goroutines that
// format text stay race-free.
var current atomic.Int32

// Current is the interface language in use.
func Current() Lang { return Lang(current.Load()) }

// Set changes the interface language. Windows show it from their next paint.
func Set(l Lang) { current.Store(int32(l)) }

// Resolve is the language a setting means; systemChinese is whether the
// system's preferred language is Chinese.
func Resolve(setting string, systemChinese bool) Lang {
	switch setting {
	case SettingChinese:
		return Chinese
	case SettingEnglish:
		return English
	}
	if systemChinese {
		return Chinese
	}
	return English
}

// T is the text in the current language.
func T(zh, en string) string {
	if Current() == English {
		return en
	}
	return zh
}

// AppName is the application's name in the current language.
func AppName() string { return T("简截", "SnapLite") }
