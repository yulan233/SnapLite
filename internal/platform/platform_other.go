//go:build !darwin && !windows

package platform

import "image"

// This adapter exists so the module builds and vets on every GOOS. Nothing
// here is supported.

func Init() error { return ErrUnsupported }

// PostMain runs fn on a new goroutine: there is no native event loop.
func PostMain(fn func()) { go fn() }

func Displays() ([]Display, error)                  { return nil, ErrUnsupported }
func CaptureDisplay(uint32) (*image.RGBA, error)    { return nil, ErrUnsupported }
func PrefetchCaptureContent()                       {}
func HasScreenCapturePermission() bool              { return false }
func RequestScreenCapturePermission()               {}
func HasAccessibilityPermission() bool              { return false }
func RequestAccessibilityPermission()               {}
func OpenPrivacySettings(string)                    {}
func RevealDirectory(string) error                  { return ErrUnsupported }
func RaiseOverlay(string, Rect) error               { return ErrUnsupported }
func FloatWindow(string, Rect) error                { return ErrUnsupported }
func ListWindows(bool) ([]Rect, error)              { return nil, ErrUnsupported }
func WindowAt(float64, float64, bool) (Rect, bool)  { return Rect{}, false }
func ActivateApp()                                  {}
func RegisterHotkey(Hotkey, func()) (func(), error) { return nil, ErrUnsupported }
func SetClipboardImage(*image.RGBA) error           { return ErrUnsupported }
func SetClipboardPNG([]byte) error                  { return ErrUnsupported }
func ClipboardChangeCount() int64                   { return 0 }
func WatchClipboard(func()) (func(), error)         { return nil, ErrUnsupported }
func ClipboardConcealed() bool                      { return false }
func CursorPosition() (float64, float64)            { return 0, 0 }
func SetLaunchAtLogin(bool) error                   { return ErrUnsupported }
func LaunchAtLogin() bool                           { return false }

func RightAltDown() bool { return false }

func SaveForegroundApp()     {}
func PasteToSavedApp() error { return ErrUnsupported }
