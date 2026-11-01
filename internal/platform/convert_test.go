package platform

import (
	"encoding/binary"
	"image"
	"testing"
)

func TestMacKeyCodes(t *testing.T) {
	cases := map[rune]uint16{'A': 0x00, 'L': 0x25, 'V': 0x09, 'Z': 0x06, '0': 0x1D, '9': 0x19}
	for key, want := range cases {
		got, ok := macKeyCode(key)
		if !ok || got != want {
			t.Errorf("macKeyCode(%q) = %#x,%v want %#x", key, got, ok, want)
		}
	}
	for r := 'A'; r <= 'Z'; r++ {
		if _, ok := macKeyCode(r); !ok {
			t.Errorf("letter %q has no key code", r)
		}
	}
	if _, ok := macKeyCode('a'); ok {
		t.Error("lower case must be rejected (callers normalise)")
	}
}

func TestModifierMasks(t *testing.T) {
	if got := macModifiers(ModCmdOrCtrl | ModShift); got != carbonCmd|carbonShift {
		t.Errorf("mac cmd+shift = %#x", got)
	}
	if got := macModifiers(ModCtrl | ModAlt); got != carbonControl|carbonOption {
		t.Errorf("mac ctrl+alt = %#x", got)
	}
	if got := winModifiers(ModCmdOrCtrl | ModShift); got != winModControl|winModShift|winModNoRepeat {
		t.Errorf("win ctrl+shift = %#x", got)
	}
	if got := winModifiers(ModCtrl | ModAlt); got != winModControl|winModAlt|winModNoRepeat {
		t.Errorf("win ctrl+alt = %#x", got)
	}
	if vk, ok := winVirtualKey('A'); !ok || vk != 0x41 {
		t.Errorf("VK_A = %#x", vk)
	}
	if vk, ok := winVirtualKey('5'); !ok || vk != 0x35 {
		t.Errorf("VK_5 = %#x", vk)
	}
	if _, ok := winVirtualKey('!'); ok {
		t.Error("punctuation must be rejected")
	}
}

func TestFunctionKeys(t *testing.T) {
	mac := map[rune]uint16{KeyF1: 0x7A, KeyF2: 0x78, KeyF3: 0x63, KeyF12: 0x6F}
	for key, want := range mac {
		if got, ok := macKeyCode(key); !ok || got != want {
			t.Errorf("macKeyCode(F%d) = %#x,%v want %#x", key-KeyF1+1, got, ok, want)
		}
	}
	if vk, ok := winVirtualKey(KeyF1); !ok || vk != 0x70 {
		t.Errorf("VK_F1 = %#x", vk)
	}
	if vk, ok := winVirtualKey(KeyF12); !ok || vk != 0x7B {
		t.Errorf("VK_F12 = %#x", vk)
	}
	if _, ok := FunctionKey(KeyF12 + 1); ok {
		t.Error("F13 is not supported")
	}
	if got := macModifiers(ModRightAlt); got != carbonOption {
		t.Errorf("mac right alt = %#x, want Option", got)
	}
}

func TestRightAltMatch(t *testing.T) {
	h := Hotkey{Mods: ModRightAlt, Key: KeyF1}
	if !rightAltMatch(h, 0x70, true, false) {
		t.Error("right Alt+F1 should match")
	}
	if rightAltMatch(h, 0x70, false, false) {
		t.Error("F1 without right Alt (left Alt or none) must not match")
	}
	if rightAltMatch(h, 0x71, true, false) {
		t.Error("right Alt+F2 must not match an F1 hotkey")
	}
	if rightAltMatch(h, 0x70, true, true) {
		t.Error("Shift was not asked for")
	}
	if !rightAltMatch(Hotkey{Mods: ModRightAlt | ModShift, Key: KeyF1}, 0x70, true, true) {
		t.Error("right Alt+Shift+F1 should match")
	}
}

func TestCocoaRectFlip(t *testing.T) {
	got := cocoaRect(Rect{X: 10, Y: 20, W: 100, H: 50}, 1000)
	if got != (Rect{X: 10, Y: 930, W: 100, H: 50}) {
		t.Fatalf("flip = %+v", got)
	}
	// A display above the primary has negative y and ends above the primary's top.
	got = cocoaRect(Rect{X: 0, Y: -1080, W: 1920, H: 1080}, 900)
	if got.Y != 900 {
		t.Fatalf("display above primary: %+v", got)
	}
}

func TestBGRAToRGBA(t *testing.T) {
	// two pixels per row, padded stride
	src := []byte{1, 2, 3, 0, 4, 5, 6, 0, 9, 9, 9, 9, 7, 8, 9, 0, 10, 11, 12, 0, 9, 9, 9, 9}
	img := bgraToRGBA(src, 2, 2, 12)
	want := []byte{3, 2, 1, 255, 6, 5, 4, 255, 9, 8, 7, 255, 12, 11, 10, 255}
	for i := range want {
		if img.Pix[i] != want[i] {
			t.Fatalf("pix = %v", img.Pix)
		}
	}
	rgba := rgbaRows([]byte{1, 2, 3, 0, 0, 0, 0, 0}, 1, 1, 8)
	if rgba.Pix[3] != 255 || rgba.Pix[0] != 1 {
		t.Fatalf("rgbaRows = %v", rgba.Pix)
	}
}

func TestDIBV5(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	copy(img.Pix, []byte{
		255, 0, 0, 255, 0, 255, 0, 255, // top row: red, green
		0, 0, 255, 128, 1, 2, 3, 4, // bottom row: blue (half alpha), misc
	})
	out := dibV5(img)
	le := binary.LittleEndian
	if len(out) != dibV5HeaderSize+16 {
		t.Fatalf("len = %d", len(out))
	}
	if le.Uint32(out[0:]) != 124 || int32(le.Uint32(out[4:])) != 2 || int32(le.Uint32(out[8:])) != 2 {
		t.Fatal("header size or dimensions")
	}
	if le.Uint16(out[14:]) != 32 || le.Uint32(out[16:]) != 3 || le.Uint32(out[52:]) != 0xFF000000 {
		t.Fatal("bit count, compression or alpha mask")
	}
	pix := out[dibV5HeaderSize:]
	// bottom-up: the first stored row is the image's bottom row, BGRA.
	if pix[0] != 255 || pix[1] != 0 || pix[2] != 0 || pix[3] != 128 {
		t.Fatalf("first stored pixel = %v", pix[:4])
	}
	if pix[8] != 0 || pix[9] != 0 || pix[10] != 255 || pix[11] != 255 {
		t.Fatalf("top-left pixel = %v", pix[8:12])
	}
}

func TestMonitorGeometry(t *testing.T) {
	primary := monitorGeometry{Physical: Rect{X: 0, Y: 0, W: 3840, H: 2160}, Scale: 2}
	right := monitorGeometry{Physical: Rect{X: 3840, Y: 0, W: 1920, H: 1080}, Scale: 1}
	if l := primary.logical(); l != (Rect{W: 1920, H: 1080}) {
		t.Fatalf("primary logical = %+v", l)
	}
	if l := right.logical(); l.X != 3840 || l.W != 1920 {
		t.Fatalf("right logical = %+v", l)
	}
	p := primary.toPhysical(Rect{X: 100, Y: 50, W: 200, H: 100})
	if p != (Rect{X: 200, Y: 100, W: 400, H: 200}) {
		t.Fatalf("toPhysical = %+v", p)
	}
	m, ok := findMonitor([]monitorGeometry{primary, right}, 4000, 10)
	if !ok || m.Scale != 1 {
		t.Fatal("findMonitor")
	}
	if _, ok := findMonitorPhysical([]monitorGeometry{primary}, 5000, 10); ok {
		t.Fatal("findMonitorPhysical outside")
	}
}

func TestMainQueue(t *testing.T) {
	var q mainQueue
	var order []int
	if !q.push(func() { order = append(order, 1); q.push(func() { order = append(order, 3) }) }) {
		t.Fatal("first push wakes")
	}
	if q.push(func() { order = append(order, 2) }) {
		t.Fatal("second push must not wake again")
	}
	q.drain()
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("order = %v", order)
	}
}

func TestWindowAtAndHotkeyTable(t *testing.T) {
	ws := []Rect{{X: 0, Y: 0, W: 100, H: 100}, {X: 0, Y: 0, W: 500, H: 500}}
	if r, ok := windowAt(ws, 50, 50); !ok || r.W != 100 {
		t.Fatal("front-most window wins")
	}
	if r, ok := windowAt(ws, 300, 300); !ok || r.W != 500 {
		t.Fatal("second window")
	}
	var table hotkeyTable
	called := false
	id := table.add(func() { called = true })
	table.get(id)()
	table.remove(id)
	if !called || table.get(id) != nil {
		t.Fatal("hotkey table")
	}
	n := 0
	f := once(func() { n++ })
	f()
	f()
	if n != 1 {
		t.Fatal("once")
	}
}
