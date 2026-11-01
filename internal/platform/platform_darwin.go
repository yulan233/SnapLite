//go:build darwin

package platform

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=14.0
#cgo LDFLAGS: -framework Cocoa -framework ScreenCaptureKit -framework CoreGraphics -framework Carbon -framework ApplicationServices -framework ServiceManagement -framework CoreMedia -framework CoreVideo
#include <stdlib.h>
#include "darwin.h"
*/
import "C"

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"sync"
	"unsafe"
)

//export goDrainMain
func goDrainMain() { queue.drain() }

//export goHotkey
func goHotkey(id C.uint32_t) {
	if fn := hotkeys.get(uint32(id)); fn != nil {
		fn()
	}
}

var clipboardWatch struct {
	mu sync.Mutex
	fn func()
}

//export goClipboardChanged
func goClipboardChanged() {
	clipboardWatch.mu.Lock()
	fn := clipboardWatch.fn
	clipboardWatch.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Init runs once on the event-loop thread inside RunApplication's start.
func Init() error {
	C.sl_init()
	return nil
}

// PostMain queues fn for the event-loop (main) thread.
func PostMain(fn func()) {
	if queue.push(fn) {
		C.sl_wake_main()
	}
}

// Displays lists the active displays.
func Displays() ([]Display, error) {
	var raw [32]C.sl_display
	n := int(C.sl_displays(&raw[0], C.int(len(raw))))
	if n < 0 {
		return nil, fmt.Errorf("platform: list displays failed")
	}
	out := make([]Display, n)
	for i := 0; i < n; i++ {
		d := raw[i]
		out[i] = Display{
			ID:      uint32(d.id),
			Bounds:  Rect{X: float64(d.x), Y: float64(d.y), W: float64(d.w), H: float64(d.h)},
			Scale:   float64(d.scale),
			Primary: d.primary != 0,
		}
	}
	return out, nil
}

func capture(id uint32) (*image.RGBA, error) {
	var pix *C.uint8_t
	var w, h, stride C.int
	status := C.sl_capture(C.uint32_t(id), &pix, &w, &h, &stride)
	switch status {
	case 0:
	case 1:
		return nil, ErrPermissionDenied
	default:
		return nil, fmt.Errorf("platform: capture display %d failed", id)
	}
	defer C.free(unsafe.Pointer(pix))
	src := unsafe.Slice((*byte)(unsafe.Pointer(pix)), int(stride)*int(h))
	return rgbaRows(src, int(w), int(h), int(stride)), nil
}

// CaptureDisplay captures a whole display in physical pixels without the
// pointer and without SnapLite's own windows. It blocks: never call it on the
// main thread.
func CaptureDisplay(id uint32) (*image.RGBA, error) { return capture(id) }

// PrefetchCaptureContent refreshes the cached shareable-content list in the
// background so the next capture starts faster.
func PrefetchCaptureContent() { C.sl_prefetch_content() }

func HasScreenCapturePermission() bool { return C.sl_has_screen_permission() != 0 }
func RequestScreenCapturePermission()  { C.sl_request_screen_permission() }
func HasAccessibilityPermission() bool { return C.sl_has_accessibility() != 0 }
func RequestAccessibilityPermission()  { C.sl_request_accessibility() }

// OpenPrivacySettings opens System Settings at the "screen" or "accessibility" pane.
func OpenPrivacySettings(pane string) {
	url := "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	if pane == "accessibility" {
		url = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	}
	s := C.CString(url)
	defer C.free(unsafe.Pointer(s))
	C.sl_open_url(s)
}

// RevealDirectory opens a folder in Finder.
func RevealDirectory(path string) error {
	s := C.CString(path)
	defer C.free(unsafe.Pointer(s))
	C.sl_reveal(s)
	return nil
}

// RaiseOverlay puts SnapLite's window titled title above everything, menu bar
// included, over bounds, and gives it the keyboard.
func RaiseOverlay(title string, bounds Rect) error {
	s := C.CString(title)
	defer C.free(unsafe.Pointer(s))
	if C.sl_raise_overlay(s, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H)) != 0 {
		return ErrNotFound
	}
	return nil
}

// FloatWindow keeps SnapLite's window titled title above other windows, on
// every Space, at bounds, without taking the keyboard from the application in
// front. Calling it again moves the window. A click lets the window become
// key, so it can receive Escape.
func FloatWindow(title string, bounds Rect) error {
	s := C.CString(title)
	defer C.free(unsafe.Pointer(s))
	if C.sl_float_window(s, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H)) != 0 {
		return ErrNotFound
	}
	return nil
}

// ListWindows returns the on-screen application windows front to back, in
// screen logical coordinates.
func ListWindows(excludeOwn bool) ([]Rect, error) {
	var raw [256]C.sl_rect
	own := C.int(0)
	if excludeOwn {
		own = 1
	}
	n := int(C.sl_list_windows(&raw[0], C.int(len(raw)), own))
	if n < 0 {
		return nil, fmt.Errorf("platform: list windows failed")
	}
	out := make([]Rect, n)
	for i := range out {
		out[i] = Rect{X: float64(raw[i].x), Y: float64(raw[i].y), W: float64(raw[i].w), H: float64(raw[i].h)}
	}
	return out, nil
}

// WindowAt returns the front-most other application's window under a point.
func WindowAt(x, y float64, excludeOwn bool) (Rect, bool) {
	ws, err := ListWindows(excludeOwn)
	if err != nil {
		return Rect{}, false
	}
	return windowAt(ws, x, y)
}

func ActivateApp() { C.sl_activate() }

// RegisterHotkey registers a global hotkey; fn runs on the main thread.
func RegisterHotkey(h Hotkey, fn func()) (func(), error) {
	code, ok := macKeyCode(h.Key)
	if !ok {
		return nil, fmt.Errorf("platform: hotkey key %q is not a letter, digit or F1–F12", h.Key)
	}
	id := hotkeys.add(fn)
	status := C.sl_register_hotkey(C.uint32_t(id), C.uint32_t(code), C.uint32_t(macModifiers(h.Mods)))
	if status != 0 {
		hotkeys.remove(id)
		if status == -9878 { // eventHotKeyExistsErr
			return nil, ErrHotkeyTaken
		}
		return nil, fmt.Errorf("platform: RegisterEventHotKey: %d", int(status))
	}
	return once(func() {
		PostMain(func() { C.sl_unregister_hotkey(C.uint32_t(id)) })
		hotkeys.remove(id)
	}), nil
}

// SetClipboardImage replaces the clipboard with img as PNG and TIFF. Main thread only.
func SetClipboardImage(img *image.RGBA) error {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return err
	}
	return SetClipboardPNG(buf.Bytes())
}

// SetClipboardPNG replaces the clipboard with already-encoded PNG bytes.
func SetClipboardPNG(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("platform: empty image")
	}
	if C.sl_set_clipboard_image(unsafe.Pointer(&data[0]), C.size_t(len(data))) != 0 {
		return fmt.Errorf("platform: writing the clipboard failed")
	}
	setOwnClipboardCount(ClipboardChangeCount())
	return nil
}

func ClipboardChangeCount() int64 { return int64(C.sl_change_count()) }

// WatchClipboard calls fn on the main thread when the clipboard changes.
func WatchClipboard(fn func()) (func(), error) {
	clipboardWatch.mu.Lock()
	clipboardWatch.fn = fn
	clipboardWatch.mu.Unlock()
	C.sl_watch_clipboard(1)
	return once(func() {
		clipboardWatch.mu.Lock()
		clipboardWatch.fn = nil
		clipboardWatch.mu.Unlock()
		PostMain(func() { C.sl_watch_clipboard(0) })
	}), nil
}

func ClipboardConcealed() bool { return C.sl_clipboard_concealed() != 0 }

// CursorPosition is the pointer in screen logical coordinates.
func CursorPosition() (float64, float64) {
	var x, y C.double
	C.sl_cursor_position(&x, &y)
	return float64(x), float64(y)
}

func SetLaunchAtLogin(enabled bool) error {
	on := C.int(0)
	if enabled {
		on = 1
	}
	switch C.sl_set_launch_at_login(on) {
	case 0:
		return nil
	case -1:
		return ErrUnsupported
	default:
		return fmt.Errorf("platform: SMAppService failed")
	}
}

func LaunchAtLogin() bool { return C.sl_launch_at_login() == 1 }

// RightAltDown is false: macOS hotkeys do not distinguish the Option keys.
func RightAltDown() bool { return false }

// SaveForegroundApp remembers the application in front (not SnapLite) as the
// target of PasteToSavedApp. Main thread only.
func SaveForegroundApp() { C.sl_save_front_app() }

// PasteToSavedApp brings the remembered application back to the front and
// presses Command+V in it, a moment later. ErrPermissionDenied means the
// Accessibility permission is missing (the clipboard is unchanged by this).
func PasteToSavedApp() error {
	if C.sl_paste_to_saved_app() != 0 {
		return ErrPermissionDenied
	}
	return nil
}
