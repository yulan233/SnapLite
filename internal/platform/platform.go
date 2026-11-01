package platform

import (
	"errors"
	"sync"
)

// Sentinel errors. Callers compare with errors.Is.
var (
	ErrUnsupported      = errors.New("platform: unsupported")
	ErrPermissionDenied = errors.New("platform: permission denied")
	ErrHotkeyTaken      = errors.New("platform: hotkey already registered")
	ErrNotFound         = errors.New("platform: window not found")
)

// Rect is a rectangle in screen logical coordinates: points from the top-left
// corner of the primary display, y down.
type Rect struct{ X, Y, W, H float64 }

// Contains reports whether the point lies inside r.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Center is the middle of r.
func (r Rect) Center() (x, y float64) { return r.X + r.W/2, r.Y + r.H/2 }

// Empty reports whether r has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Display is one monitor.
type Display struct {
	ID uint32
	// Bounds is the whole display, menu bar and taskbar included, in screen
	// logical coordinates (the coordinates Viva's Window.Frame, Config.Origin
	// and PointerEvent.ScreenX/Y use on this platform).
	Bounds Rect
	// Scale is physical pixels per logical pixel. The capture's own pixel width
	// divided by Bounds.W is authoritative.
	Scale   float64
	Primary bool
}

// Modifiers of a global hotkey.
type Modifiers uint8

const (
	// ModCmdOrCtrl is Command on macOS and Control on Windows.
	ModCmdOrCtrl Modifiers = 1 << iota
	ModShift
	ModAlt
	// ModCtrl is the Control key itself; on Windows it is the same as ModCmdOrCtrl.
	ModCtrl
	// ModRightAlt is the right-hand Alt key alone on Windows (the left one does
	// not trigger the hotkey). macOS cannot tell the Option keys apart in a
	// global hotkey, so there it means either Option key, like ModAlt.
	ModRightAlt
)

// Function keys use AppKit's NSF1FunctionKey.. code points, so a Hotkey.Key
// stays a single rune.
const (
	KeyF1 rune = 0xF704 + iota
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// FunctionKey reports the number (1–12) of a function-key rune.
func FunctionKey(key rune) (int, bool) {
	if key >= KeyF1 && key <= KeyF12 {
		return int(key-KeyF1) + 1, true
	}
	return 0, false
}

// Hotkey is a global shortcut: modifiers plus an upper-case letter, a digit or
// a function key (KeyF1..KeyF12).
type Hotkey struct {
	Mods Modifiers
	Key  rune
}

// mainQueue is the cross-thread task queue PostMain fills. The native side
// is only woken when the queue goes from empty to non-empty.
type mainQueue struct {
	mu    sync.Mutex
	tasks []func()
}

// push appends fn and reports whether the queue was empty before.
func (q *mainQueue) push(fn func()) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, fn)
	return len(q.tasks) == 1
}

// drain runs every queued task in order, including ones queued meanwhile.
func (q *mainQueue) drain() {
	for {
		q.mu.Lock()
		tasks := q.tasks
		q.tasks = nil
		q.mu.Unlock()
		if len(tasks) == 0 {
			return
		}
		for _, task := range tasks {
			task()
		}
	}
}

var queue mainQueue

// hotkeyTable maps native hotkey identifiers to callbacks. It is only used on
// the event-loop thread, but guarded anyway because unregister funcs may be
// called from anywhere.
type hotkeyTable struct {
	mu   sync.Mutex
	next uint32
	fns  map[uint32]func()
}

func (t *hotkeyTable) add(fn func()) uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fns == nil {
		t.fns = map[uint32]func(){}
	}
	t.next++
	t.fns[t.next] = fn
	return t.next
}

func (t *hotkeyTable) remove(id uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fns, id)
}

func (t *hotkeyTable) get(id uint32) func() {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fns[id]
}

var hotkeys hotkeyTable

// clipboardOwn remembers the change count our own SetClipboardImage produced,
// so WatchClipboard can skip it.
var clipboardOwn struct {
	mu    sync.Mutex
	count int64
}

func setOwnClipboardCount(n int64) {
	clipboardOwn.mu.Lock()
	clipboardOwn.count = n
	clipboardOwn.mu.Unlock()
}

// OwnClipboardChange reports whether the clipboard's current change count is
// the one SnapLite itself produced last.
func OwnClipboardChange() bool {
	clipboardOwn.mu.Lock()
	defer clipboardOwn.mu.Unlock()
	return clipboardOwn.count != 0 && clipboardOwn.count == ClipboardChangeCount()
}

// once wraps fn so calling the result more than once does nothing.
func once(fn func()) func() {
	var o sync.Once
	return func() { o.Do(fn) }
}
