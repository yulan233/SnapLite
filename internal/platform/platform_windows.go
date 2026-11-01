//go:build windows

package platform

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Pure Go Win32 adapter, the same approach Viva's own Windows backend takes:
// lazily loaded DLL procedures, no cgo.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procEnumThreadWindows             = user32.NewProc("EnumThreadWindows")
	procEnumWindows                   = user32.NewProc("EnumWindows")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowLongPtrW             = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW             = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procSetFocus                      = user32.NewProc("SetFocus")
	procKeybdEvent                    = user32.NewProc("keybd_event")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procEmptyClipboard                = user32.NewProc("EmptyClipboard")
	procSetClipboardData              = user32.NewProc("SetClipboardData")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procRegisterClipboardFormatW      = user32.NewProc("RegisterClipboardFormatW")
	procIsClipboardFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNumber    = user32.NewProc("GetClipboardSequenceNumber")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procSendInput                     = user32.NewProc("SendInput")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procGetThreadDpiAwarenessContext  = user32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual  = user32.NewProc("AreDpiAwarenessContextsEqual")
	procSetThreadDpiAwarenessContext  = user32.NewProc("SetThreadDpiAwarenessContext")
	procGetAsyncKeyState              = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procIsWindow                      = user32.NewProc("IsWindow")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")

	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
	procGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	procGlobalFree         = kernel32.NewProc("GlobalFree")
	procGlobalLock         = kernel32.NewProc("GlobalLock")
	procGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
	procGlobalSize         = kernel32.NewProc("GlobalSize")

	procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

	procGetDpiForMonitor      = shcore.NewProc("GetDpiForMonitor")
	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
	procShellExecuteW         = shell32.NewProc("ShellExecuteW")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

const (
	wmHotkey          = 0x0312
	wmClipboardUpdate = 0x031D
	wmApp             = 0x8000
	wmDrainMain       = wmApp + 1

	gwlExStyle      = ^uintptr(19) // GWL_EXSTYLE = -20
	wsExToolWindow  = 0x00000080
	wsExTopmost     = 0x00000008
	wsExAppWindow   = 0x00040000
	swpShowWindow   = 0x0040
	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010
	hwndTopmost     = ^uintptr(0) // (HWND)-1
	hwndMessage     = ^uintptr(2) // (HWND)-3

	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000

	cfDIBV5         = 17
	gmemMoveable    = 0x0002
	errHotkeyExists = 1409

	vkMenu          = 0x12
	keyEventFKeyUp  = 0x0002
	dwmExtendedRect = 9
	dwmCloaked      = 14
	monitorPrimary  = 1
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type windowClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// pointer turns an address returned by a Win32 call into an unsafe.Pointer
// without the uintptr→Pointer conversion go vet rejects.
func pointer(address uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&address))
}

// ---- lifecycle -------------------------------------------------------------

var (
	messageWindow uintptr
	clipboardFn   func()
	clipboardMu   sync.Mutex
)

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmDrainMain:
		queue.drain()
		return 0
	case wmHotkey:
		if fn := hotkeys.get(uint32(wParam)); fn != nil {
			fn()
		}
		return 0
	case wmClipboardUpdate:
		clipboardMu.Lock()
		fn := clipboardFn
		clipboardMu.Unlock()
		if fn != nil {
			fn()
		}
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return result
}

// Init creates SnapLite's message-only window on the event-loop thread.
// Viva's GetMessageW(0) loop on the same thread dispatches its messages.
func Init() error {
	ensureDPIAwareness()
	name, _ := syscall.UTF16PtrFromString("SnapLiteMessageWindow")
	instance, _, _ := procGetModuleHandleW.Call(0)
	class := windowClassEx{WndProc: syscall.NewCallback(windowProc), Instance: instance, ClassName: name}
	class.Size = uint32(unsafe.Sizeof(class))
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("platform: RegisterClassExW: %w", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("platform: CreateWindowExW: %w", err)
	}
	messageWindow = hwnd
	return nil
}

func ensureDPIAwareness() {
	if procGetThreadDpiAwarenessContext.Find() != nil || procSetThreadDpiAwarenessContext.Find() != nil {
		return
	}
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	current, _, _ := procGetThreadDpiAwarenessContext.Call()
	if equal, _, _ := procAreDpiAwarenessContextsEqual.Call(current, perMonitorV2); equal == 0 {
		procSetThreadDpiAwarenessContext.Call(perMonitorV2)
	}
}

// PostMain queues fn for the event-loop thread.
func PostMain(fn func()) {
	if queue.push(fn) && messageWindow != 0 {
		procPostMessageW.Call(messageWindow, wmDrainMain, 0, 0)
	}
}

// ---- displays --------------------------------------------------------------

type winMonitor struct {
	handle   uintptr
	geometry monitorGeometry
	primary  bool
}

var monitors struct {
	mu   sync.Mutex
	list []winMonitor
}

// syscall.NewCallback slots are never released, so every enumeration
// callback is created once and reports into a package-level collector that a
// mutex serialises.
var (
	callbacksOnce  sync.Once
	monitorProc    uintptr
	threadWindowCb uintptr
	topWindowCb    uintptr

	enumMu          sync.Mutex
	enumMonitorsOut []winMonitor
	enumTitle       string
	enumFound       uintptr
	enumWindows     []Rect
	enumOwn         bool
)

func initCallbacks() {
	callbacksOnce.Do(func() {
		monitorProc = syscall.NewCallback(monitorCallback)
		threadWindowCb = syscall.NewCallback(threadWindowCallback)
		topWindowCb = syscall.NewCallback(topWindowCallback)
	})
}

func monitorCallback(handle, dc, area, data uintptr) uintptr {
	info := monitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := procGetMonitorInfoW.Call(handle, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 1
	}
	scale := 1.0
	if procGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if hr, _, _ := procGetDpiForMonitor.Call(handle, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); hr == 0 && dx > 0 {
			scale = float64(dx) / 96
		}
	}
	m := info.Monitor
	enumMonitorsOut = append(enumMonitorsOut, winMonitor{
		handle: handle,
		geometry: monitorGeometry{
			Physical: Rect{X: float64(m.Left), Y: float64(m.Top), W: float64(m.Right - m.Left), H: float64(m.Bottom - m.Top)},
			Scale:    scale,
		},
		primary: info.Flags&monitorPrimary != 0,
	})
	return 1
}

func enumMonitors() ([]winMonitor, error) {
	initCallbacks()
	enumMu.Lock()
	enumMonitorsOut = nil
	ok, _, err := procEnumDisplayMonitors.Call(0, 0, monitorProc, 0)
	list := enumMonitorsOut
	enumMonitorsOut = nil
	enumMu.Unlock()
	if ok == 0 {
		return nil, fmt.Errorf("platform: EnumDisplayMonitors: %w", err)
	}
	monitors.mu.Lock()
	monitors.list = list
	monitors.mu.Unlock()
	return list, nil
}

func cachedMonitors() []monitorGeometry {
	monitors.mu.Lock()
	list := monitors.list
	monitors.mu.Unlock()
	if len(list) == 0 {
		list, _ = enumMonitors()
	}
	out := make([]monitorGeometry, len(list))
	for i, m := range list {
		out[i] = m.geometry
	}
	return out
}

// Displays lists the monitors. ID is the enumeration index (HMONITOR does not
// fit a uint32 safely); Bounds are in Viva's Windows logical coordinates,
// physical pixels divided by that monitor's own scale.
func Displays() ([]Display, error) {
	list, err := enumMonitors()
	if err != nil {
		return nil, err
	}
	out := make([]Display, len(list))
	for i, m := range list {
		out[i] = Display{ID: uint32(i), Bounds: m.geometry.logical(), Scale: m.geometry.Scale, Primary: m.primary}
	}
	return out, nil
}

func monitorByID(id uint32) (monitorGeometry, error) {
	monitors.mu.Lock()
	list := monitors.list
	monitors.mu.Unlock()
	if int(id) >= len(list) {
		var err error
		if list, err = enumMonitors(); err != nil {
			return monitorGeometry{}, err
		}
	}
	if int(id) >= len(list) {
		return monitorGeometry{}, fmt.Errorf("platform: display %d not found", id)
	}
	return list[id].geometry, nil
}

// ---- capture (GDI) ---------------------------------------------------------

func capturePhysical(r Rect) (*image.RGBA, error) {
	x, y, w, h := int32(r.X), int32(r.Y), int32(r.W), int32(r.H)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("platform: empty capture rectangle")
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("platform: GetDC failed")
	}
	defer procReleaseDC.Call(0, screen)
	memory, _, _ := procCreateCompatibleDC.Call(screen)
	if memory == 0 {
		return nil, fmt.Errorf("platform: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memory)
	header := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	var bits uintptr
	bitmap, _, _ := procCreateDIBSection.Call(memory, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == 0 {
		return nil, fmt.Errorf("platform: CreateDIBSection failed")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memory, bitmap)
	defer procSelectObject.Call(memory, old)
	if ok, _, err := procBitBlt.Call(memory, 0, 0, uintptr(w), uintptr(h), screen, uintptr(x), uintptr(y), srcCopy|captureBlt); ok == 0 {
		return nil, fmt.Errorf("platform: BitBlt: %w", err)
	}
	src := unsafe.Slice((*byte)(pointer(bits)), int(w)*int(h)*4)
	return bgraToRGBA(src, int(w), int(h), int(w)*4), nil
}

func CaptureDisplay(id uint32) (*image.RGBA, error) {
	m, err := monitorByID(id)
	if err != nil {
		return nil, err
	}
	return capturePhysical(m.Physical)
}

func PrefetchCaptureContent()           {}
func HasScreenCapturePermission() bool  { return true }
func RequestScreenCapturePermission()   {}
func HasAccessibilityPermission() bool  { return true }
func RequestAccessibilityPermission()   {}
func OpenPrivacySettings(string)        {}
func RevealDirectory(path string) error { return shellOpen(path) }

func shellOpen(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1); r <= 32 {
		return fmt.Errorf("platform: ShellExecuteW failed: %d", r)
	}
	return nil
}

// ---- windows ---------------------------------------------------------------

func windowText(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// findWindow looks for a window of this thread (Viva creates its windows on
// the event-loop thread) by its unique title.
func findWindow(title string) uintptr {
	initCallbacks()
	enumMu.Lock()
	defer enumMu.Unlock()
	enumTitle, enumFound = title, 0
	thread, _, _ := procGetCurrentThreadId.Call()
	procEnumThreadWindows.Call(thread, threadWindowCb, 0)
	return enumFound
}

func threadWindowCallback(hwnd, data uintptr) uintptr {
	if windowText(hwnd) == enumTitle {
		enumFound = hwnd
		return 0
	}
	return 1
}

func RaiseOverlay(title string, bounds Rect) error {
	hwnd := findWindow(title)
	if hwnd == 0 {
		return ErrNotFound
	}
	placeWindow(hwnd, bounds, swpShowWindow|swpFrameChanged)
	// The foreground lock is lifted for a process that just received input;
	// a synthetic Alt press counts, which is the classic way to take the
	// foreground from a hotkey or tray handler.
	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procKeybdEvent.Call(vkMenu, 0, keyEventFKeyUp, 0)
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(hwnd)
	return nil
}

// FloatWindow keeps the window topmost at bounds without activating it.
func FloatWindow(title string, bounds Rect) error {
	hwnd := findWindow(title)
	if hwnd == 0 {
		return ErrNotFound
	}
	placeWindow(hwnd, bounds, swpShowWindow|swpFrameChanged|swpNoActivate)
	return nil
}

// placeWindow makes hwnd a topmost tool window covering the logical bounds,
// in that display's physical pixels.
//
// Moving a window onto a display of another scale makes Windows send
// WM_DPICHANGED, and Viva then applies its suggested rectangle: the old size
// times the ratio of the scales. An overlay moved from a 100% primary onto a
// 125% secondary would come out 25% too large, its picture stretched past the
// screen's edges. Once the scale has changed the window is placed again,
// right away (the message is sent during SetWindowPos) and once more on the
// next loop turn in case it was posted instead.
func placeWindow(hwnd uintptr, bounds Rect, flags uintptr) {
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	style = (style | wsExToolWindow | wsExTopmost) &^ wsExAppWindow
	procSetWindowLongPtrW.Call(hwnd, gwlExStyle, style)
	phys := bounds
	if m, ok := monitorForBounds(cachedMonitors(), bounds); ok {
		phys = m.toPhysical(bounds)
	}
	setWindowRect(hwnd, phys, flags)
	if !windowRectIs(hwnd, phys) {
		setWindowRect(hwnd, phys, flags)
	}
	PostMain(func() {
		if ok, _, _ := procIsWindow.Call(hwnd); ok != 0 && !windowRectIs(hwnd, phys) {
			setWindowRect(hwnd, phys, flags|swpNoActivate)
		}
	})
}

func setWindowRect(hwnd uintptr, phys Rect, flags uintptr) {
	procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(int32(phys.X)), uintptr(int32(phys.Y)), uintptr(int32(phys.W)), uintptr(int32(phys.H)), flags)
}

// windowRectIs reports whether the window's rectangle is phys.
func windowRectIs(hwnd uintptr, phys Rect) bool {
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return true
	}
	return r.Left == int32(phys.X) && r.Top == int32(phys.Y) && r.Right-r.Left == int32(phys.W) && r.Bottom-r.Top == int32(phys.H)
}

func windowBounds(hwnd uintptr, geometry []monitorGeometry) (Rect, bool) {
	var r rect
	if procDwmGetWindowAttribute.Find() == nil {
		if hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmExtendedRect, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); hr != 0 {
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		}
	} else {
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	}
	phys := Rect{X: float64(r.Left), Y: float64(r.Top), W: float64(r.Right - r.Left), H: float64(r.Bottom - r.Top)}
	if phys.W < 40 || phys.H < 40 {
		return Rect{}, false
	}
	m, ok := findMonitorPhysical(geometry, phys.X+phys.W/2, phys.Y+phys.H/2)
	if !ok {
		return Rect{}, false
	}
	s := m.Scale
	l := m.logical()
	return Rect{X: l.X + (phys.X-m.Physical.X)/s, Y: l.Y + (phys.Y-m.Physical.Y)/s, W: phys.W / s, H: phys.H / s}, true
}

func ownWindow(hwnd uintptr) bool {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return int(pid) == os.Getpid()
}

func ListWindows(excludeOwn bool) ([]Rect, error) {
	initCallbacks()
	monitorList := cachedMonitors()
	enumMu.Lock()
	enumWindows, enumOwn, enumGeometry = nil, excludeOwn, monitorList
	ok, _, err := procEnumWindows.Call(topWindowCb, 0)
	out := enumWindows
	enumWindows = nil
	enumMu.Unlock()
	if ok == 0 {
		return nil, fmt.Errorf("platform: EnumWindows: %w", err)
	}
	return out, nil
}

var enumGeometry []monitorGeometry

func topWindowCallback(hwnd, data uintptr) uintptr {
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return 1
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return 1
	}
	if enumOwn && ownWindow(hwnd) {
		return 1
	}
	if procDwmGetWindowAttribute.Find() == nil {
		var cloaked uint32
		if hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmCloaked, uintptr(unsafe.Pointer(&cloaked)), 4); hr == 0 && cloaked != 0 {
			return 1
		}
	}
	if r, ok := windowBounds(hwnd, enumGeometry); ok {
		enumWindows = append(enumWindows, r)
	}
	return 1
}

func WindowAt(x, y float64, excludeOwn bool) (Rect, bool) {
	ws, err := ListWindows(excludeOwn)
	if err != nil {
		return Rect{}, false
	}
	return windowAt(ws, x, y)
}

func ActivateApp() {}

// ---- hotkeys ---------------------------------------------------------------

func RegisterHotkey(h Hotkey, fn func()) (func(), error) {
	vk, ok := winVirtualKey(h.Key)
	if !ok {
		return nil, fmt.Errorf("platform: hotkey key %q is not a letter, digit or F1–F12", h.Key)
	}
	if messageWindow == 0 {
		return nil, errors.New("platform: Init has not run")
	}
	if h.Mods&ModRightAlt != 0 {
		return registerRightAltHotkey(h, fn)
	}
	id := hotkeys.add(fn)
	if ok, _, err := procRegisterHotKey.Call(messageWindow, uintptr(id), uintptr(winModifiers(h.Mods)), uintptr(vk)); ok == 0 {
		hotkeys.remove(id)
		if errno, isErrno := err.(syscall.Errno); isErrno && errno == errHotkeyExists {
			return nil, ErrHotkeyTaken
		}
		return nil, fmt.Errorf("platform: RegisterHotKey: %w", err)
	}
	return once(func() {
		PostMain(func() { procUnregisterHotKey.Call(messageWindow, uintptr(id)) })
		hotkeys.remove(id)
	}), nil
}

// RegisterHotKey cannot tell the two Alt keys apart, so a right-Alt hotkey is
// registered as an Alt hotkey whose handler checks that the right Alt key is
// the one held. A low-level keyboard hook could let left Alt+key through
// instead, but a global keyboard hook in a program that also captures the
// screen and watches the clipboard is what antivirus heuristics flag as a
// keylogger (360 reported Trojan.Generic), so left Alt with the same key
// does nothing while SnapLite holds the hotkey.

const (
	vkRightMenu      = 0xA5
	vkNoName         = 0xFC // VK_NONAME: an unassigned key
	inputKeyboard    = 1
	keyEventFKeyUpLL = 0x0002
)

type keybdInput struct {
	VK        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// keyboardInputRecord is INPUT with its union sized like the mouse member, the
// largest, so SendInput accepts its cbSize.
type keyboardInputRecord struct {
	Type uint32
	Key  keybdInput
	_    [8]byte
}

func keyDown(vk uintptr) bool {
	state, _, _ := procGetAsyncKeyState.Call(vk)
	return state&0x8000 != 0
}

// suppressAltMenu taps an unassigned key so that releasing Alt afterwards does
// not open the foreground window's menu bar.
func suppressAltMenu() {
	events := [2]keyboardInputRecord{
		{Type: inputKeyboard, Key: keybdInput{VK: vkNoName}},
		{Type: inputKeyboard, Key: keybdInput{VK: vkNoName, Flags: keyEventFKeyUpLL}},
	}
	procSendInput.Call(2, uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
}

func registerRightAltHotkey(h Hotkey, fn func()) (func(), error) {
	onRightAlt := func() {
		if keyDown(vkRightMenu) {
			suppressAltMenu()
			fn()
		}
	}
	alt, altGr := rightAltVariants(h)
	unregister, err := RegisterHotkey(alt, onRightAlt)
	if err != nil {
		return nil, err
	}
	// The AltGr variant is a convenience: when another program holds it, the
	// plain one still works.
	unregisterAltGr, err := RegisterHotkey(altGr, onRightAlt)
	if err != nil {
		return unregister, nil
	}
	return once(func() {
		unregister()
		unregisterAltGr()
	}), nil
}

// ---- clipboard -------------------------------------------------------------

func registerFormat(name string) uintptr {
	p, _ := syscall.UTF16PtrFromString(name)
	format, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return format
}

func openClipboard() error {
	for attempt := 0; attempt < 5; attempt++ {
		if ok, _, _ := procOpenClipboard.Call(messageWindow); ok != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("platform: the clipboard is in use by another program")
}

func globalBytes(data []byte) (uintptr, error) {
	handle, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if handle == 0 {
		return 0, fmt.Errorf("platform: GlobalAlloc: %w", err)
	}
	address, _, _ := procGlobalLock.Call(handle)
	if address == 0 {
		procGlobalFree.Call(handle)
		return 0, errors.New("platform: GlobalLock failed")
	}
	copy(unsafe.Slice((*byte)(pointer(address)), len(data)), data)
	procGlobalUnlock.Call(handle)
	return handle, nil
}

func SetClipboardImage(img *image.RGBA) error {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return err
	}
	return setClipboard(dibV5(img), buf.Bytes())
}

func SetClipboardPNG(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		b := img.Bounds()
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				rgba.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	}
	return setClipboard(dibV5(rgba), data)
}

func setClipboard(dib, pngData []byte) error {
	if err := openClipboard(); err != nil {
		return err
	}
	// The sequence number moves when the clipboard closes, so SnapLite's own
	// change is recorded after closing it (deferred calls run last-first).
	defer func() {
		procCloseClipboard.Call()
		setOwnClipboardCount(ClipboardChangeCount())
	}()
	procEmptyClipboard.Call()
	dibHandle, err := globalBytes(dib)
	if err != nil {
		return err
	}
	if ok, _, err := procSetClipboardData.Call(cfDIBV5, dibHandle); ok == 0 {
		procGlobalFree.Call(dibHandle)
		return fmt.Errorf("platform: SetClipboardData(CF_DIBV5): %w", err)
	}
	if pngHandle, err := globalBytes(pngData); err == nil {
		if ok, _, _ := procSetClipboardData.Call(registerFormat("PNG"), pngHandle); ok == 0 {
			procGlobalFree.Call(pngHandle)
		}
	}
	return nil
}

func ClipboardChangeCount() int64 {
	n, _, _ := procGetClipboardSequenceNumber.Call()
	return int64(uint32(n))
}

// WatchClipboard is event driven: WM_CLIPBOARDUPDATE reaches the message window.
func WatchClipboard(fn func()) (func(), error) {
	if messageWindow == 0 {
		return nil, errors.New("platform: Init has not run")
	}
	clipboardMu.Lock()
	clipboardFn = fn
	clipboardMu.Unlock()
	if ok, _, err := procAddClipboardFormatListener.Call(messageWindow); ok == 0 {
		return nil, fmt.Errorf("platform: AddClipboardFormatListener: %w", err)
	}
	return once(func() {
		clipboardMu.Lock()
		clipboardFn = nil
		clipboardMu.Unlock()
		PostMain(func() { procRemoveClipboardFormatListener.Call(messageWindow) })
	}), nil
}

func ClipboardConcealed() bool {
	for _, name := range []string{"ExcludeClipboardContentFromMonitorProcessing", "Clipboard Viewer Ignore"} {
		if ok, _, _ := procIsClipboardFormatAvailable.Call(registerFormat(name)); ok != 0 {
			return true
		}
	}
	history := registerFormat("CanIncludeInClipboardHistory")
	if ok, _, _ := procIsClipboardFormatAvailable.Call(history); ok == 0 {
		return false
	}
	if openClipboard() != nil {
		return false
	}
	defer procCloseClipboard.Call()
	handle, _, _ := procGetClipboardData.Call(history)
	if handle == 0 {
		return false
	}
	if size, _, _ := procGlobalSize.Call(handle); size < 4 {
		return false
	}
	address, _, _ := procGlobalLock.Call(handle)
	if address == 0 {
		return false
	}
	value := *(*uint32)(pointer(address))
	procGlobalUnlock.Call(handle)
	return value == 0
}

// ---- pointer position -------------------------------------------------------

func CursorPosition() (float64, float64) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if m, ok := findMonitorPhysical(cachedMonitors(), float64(p.X), float64(p.Y)); ok {
		l := m.logical()
		return l.X + (float64(p.X)-m.Physical.X)/m.Scale, l.Y + (float64(p.Y)-m.Physical.Y)/m.Scale
	}
	return float64(p.X), float64(p.Y)
}

// ---- launch at login (HKCU\...\Run) ----------------------------------------

const (
	hkeyCurrentUser = 0x80000001
	keySetValue     = 0x0002
	keyQueryValue   = 0x0001
	regSZ           = 1
	runKey          = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue        = "简截"
)

func openRunKey(access uintptr) (uintptr, error) {
	path, _ := syscall.UTF16PtrFromString(runKey)
	var key uintptr
	if status, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, access, uintptr(unsafe.Pointer(&key))); status != 0 {
		return 0, fmt.Errorf("platform: RegOpenKeyExW: %d", status)
	}
	return key, nil
}

func SetLaunchAtLogin(enabled bool) error {
	key, err := openRunKey(keySetValue)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(key)
	name, _ := syscall.UTF16PtrFromString(runValue)
	if !enabled {
		procRegDeleteValueW.Call(key, uintptr(unsafe.Pointer(name)))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	value, _ := syscall.UTF16FromString(`"` + exe + `" --background`)
	if status, _, _ := procRegSetValueExW.Call(key, uintptr(unsafe.Pointer(name)), 0, regSZ, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2)); status != 0 {
		return fmt.Errorf("platform: RegSetValueExW: %d", status)
	}
	return nil
}

func LaunchAtLogin() bool {
	key, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer procRegCloseKey.Call(key)
	name, _ := syscall.UTF16PtrFromString(runValue)
	status, _, _ := procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0)
	return status == 0
}

// PrefersChinese reports whether the user interface language is Chinese
// (primary language LANG_CHINESE, any region).
func PrefersChinese() bool {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	return lang&0x3FF == 0x04
}

// RightAltDown reports whether the right Alt key is held now. A window that
// records a hotkey uses it, since key events do not say which Alt is down.
func RightAltDown() bool { return keyDown(vkRightMenu) }

// ---- quick paste -------------------------------------------------------------

var savedForeground uintptr

// SaveForegroundApp remembers the window in front (not SnapLite's) as the
// target of PasteToSavedApp.
func SaveForegroundApp() {
	if hwnd, _, _ := procGetForegroundWindow.Call(); hwnd != 0 && !ownWindow(hwnd) {
		savedForeground = hwnd
	}
}

const vkControl = 0x11

// PasteToSavedApp brings the remembered window back to the front and presses
// Control+V in it a moment later, when it has the keyboard.
func PasteToSavedApp() error {
	if savedForeground != 0 {
		procSetForegroundWindow.Call(savedForeground)
	}
	go func() {
		time.Sleep(120 * time.Millisecond)
		events := [4]keyboardInputRecord{
			{Type: inputKeyboard, Key: keybdInput{VK: vkControl}},
			{Type: inputKeyboard, Key: keybdInput{VK: 'V'}},
			{Type: inputKeyboard, Key: keybdInput{VK: 'V', Flags: keyEventFKeyUpLL}},
			{Type: inputKeyboard, Key: keybdInput{VK: vkControl, Flags: keyEventFKeyUpLL}},
		}
		procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	}()
	return nil
}
