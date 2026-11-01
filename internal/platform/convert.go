package platform

import (
	"encoding/binary"
	"image"
	"math"
)

// The pure functions in this file are used by the native adapters and are
// kept free of build tags so their tests run on every host.

// macKeyCodes maps letters and digits to the kVK_ANSI_* virtual key codes of
// the Carbon HIToolbox Events.h header.
var macKeyCodes = map[rune]uint16{
	'A': 0x00, 'S': 0x01, 'D': 0x02, 'F': 0x03, 'H': 0x04, 'G': 0x05, 'Z': 0x06, 'X': 0x07,
	'C': 0x08, 'V': 0x09, 'B': 0x0B, 'Q': 0x0C, 'W': 0x0D, 'E': 0x0E, 'R': 0x0F, 'Y': 0x10,
	'T': 0x11, '1': 0x12, '2': 0x13, '3': 0x14, '4': 0x15, '6': 0x16, '5': 0x17, '9': 0x19,
	'7': 0x1A, '8': 0x1C, '0': 0x1D, 'O': 0x1F, 'U': 0x20, 'I': 0x22, 'P': 0x23, 'L': 0x25,
	'J': 0x26, 'K': 0x28, 'N': 0x2D, 'M': 0x2E,
}

// macFunctionKeyCodes are kVK_F1..kVK_F12, which are not contiguous.
var macFunctionKeyCodes = [12]uint16{0x7A, 0x78, 0x63, 0x76, 0x60, 0x61, 0x62, 0x64, 0x65, 0x6D, 0x67, 0x6F}

// macKeyCode returns the Carbon virtual key code of an upper-case letter, a
// digit or a function key.
func macKeyCode(key rune) (uint16, bool) {
	if n, ok := FunctionKey(key); ok {
		return macFunctionKeyCodes[n-1], true
	}
	code, ok := macKeyCodes[key]
	return code, ok
}

// Carbon modifier masks (Events.h).
const (
	carbonCmd     = 0x0100
	carbonShift   = 0x0200
	carbonOption  = 0x0800
	carbonControl = 0x1000
)

// macModifiers converts hotkey modifiers to Carbon's modifier mask.
func macModifiers(m Modifiers) uint32 {
	var out uint32
	if m&ModCmdOrCtrl != 0 {
		out |= carbonCmd
	}
	if m&ModShift != 0 {
		out |= carbonShift
	}
	if m&(ModAlt|ModRightAlt) != 0 {
		out |= carbonOption
	}
	if m&ModCtrl != 0 {
		out |= carbonControl
	}
	return out
}

// winVirtualKey returns the Win32 virtual key of an upper-case letter, a digit
// (VK_A..VK_Z equal 'A'..'Z', VK_0..VK_9 equal '0'..'9') or a function key
// (VK_F1 = 0x70 ..).
func winVirtualKey(key rune) (uint16, bool) {
	if n, ok := FunctionKey(key); ok {
		return uint16(0x70 + n - 1), true
	}
	switch {
	case key >= 'A' && key <= 'Z', key >= '0' && key <= '9':
		return uint16(key), true
	}
	return 0, false
}

// rightAltVariants are the hotkeys a right-Alt hotkey is registered as on
// Windows: plain Alt, and Control+Alt for AltGr layouts, which report the
// right Alt key as both. The handler then checks which Alt key is down.
func rightAltVariants(h Hotkey) (alt, altGr Hotkey) {
	alt = Hotkey{Mods: h.Mods&^ModRightAlt | ModAlt, Key: h.Key}
	return alt, Hotkey{Mods: alt.Mods | ModCtrl, Key: h.Key}
}

// Win32 RegisterHotKey modifier flags.
const (
	winModAlt      = 0x0001
	winModControl  = 0x0002
	winModShift    = 0x0004
	winModNoRepeat = 0x4000
)

// winModifiers converts hotkey modifiers to RegisterHotKey flags. Command does
// not exist on Windows, so ModCmdOrCtrl and ModCtrl both mean Control.
func winModifiers(m Modifiers) uint32 {
	out := uint32(winModNoRepeat)
	if m&(ModCmdOrCtrl|ModCtrl) != 0 {
		out |= winModControl
	}
	if m&ModShift != 0 {
		out |= winModShift
	}
	if m&ModAlt != 0 {
		out |= winModAlt
	}
	return out
}

// cocoaRect converts a top-left-origin screen rectangle to AppKit's
// bottom-left-origin screen space, where primaryHeight is the primary
// display's height in points.
func cocoaRect(r Rect, primaryHeight float64) Rect {
	return Rect{X: r.X, Y: primaryHeight - (r.Y + r.H), W: r.W, H: r.H}
}

// bgraToRGBA copies width×height BGRA (or BGRX) pixels with the given row
// stride into an opaque *image.RGBA.
func bgraToRGBA(src []byte, width, height, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		in := src[y*stride : y*stride+width*4]
		out := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < width*4; x += 4 {
			out[x+0] = in[x+2]
			out[x+1] = in[x+1]
			out[x+2] = in[x+0]
			out[x+3] = 255
		}
	}
	return img
}

// rgbaRows copies a top-down RGBA buffer with the given stride into a new
// *image.RGBA, forcing alpha to opaque (screen pixels have no transparency).
func rgbaRows(src []byte, width, height, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		out := img.Pix[y*img.Stride : y*img.Stride+width*4]
		copy(out, src[y*stride:y*stride+width*4])
		for x := 3; x < len(out); x += 4 {
			out[x] = 255
		}
	}
	return img
}

// dibV5HeaderSize is sizeof(BITMAPV5HEADER).
const dibV5HeaderSize = 124

// dibV5 encodes img as a CF_DIBV5 clipboard payload: a BITMAPV5HEADER with
// BI_BITFIELDS masks and sRGB colour space, followed by bottom-up 32-bit BGRA
// rows with straight alpha.
func dibV5(img *image.RGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	size := w * h * 4
	out := make([]byte, dibV5HeaderSize+size)
	le := binary.LittleEndian
	le.PutUint32(out[0:], dibV5HeaderSize)
	le.PutUint32(out[4:], uint32(int32(w)))
	le.PutUint32(out[8:], uint32(int32(h))) // positive: bottom-up
	le.PutUint16(out[12:], 1)               // planes
	le.PutUint16(out[14:], 32)              // bit count
	le.PutUint32(out[16:], 3)               // BI_BITFIELDS
	le.PutUint32(out[20:], uint32(size))
	le.PutUint32(out[24:], 2835) // 72 DPI in pixels per metre
	le.PutUint32(out[28:], 2835)
	le.PutUint32(out[40:], 0x00FF0000) // red mask
	le.PutUint32(out[44:], 0x0000FF00) // green mask
	le.PutUint32(out[48:], 0x000000FF) // blue mask
	le.PutUint32(out[52:], 0xFF000000) // alpha mask
	le.PutUint32(out[56:], 0x73524742) // LCS_sRGB 'sRGB'
	le.PutUint32(out[108:], 4)         // LCS_GM_IMAGES intent
	pix := out[dibV5HeaderSize:]
	for y := 0; y < h; y++ {
		in := img.Pix[(y)*img.Stride : y*img.Stride+w*4]
		row := pix[(h-1-y)*w*4 : (h-y)*w*4]
		for x := 0; x < w*4; x += 4 {
			row[x+0] = in[x+2]
			row[x+1] = in[x+1]
			row[x+2] = in[x+0]
			row[x+3] = in[x+3]
		}
	}
	return out
}

// monitorGeometry is a display in physical pixels with its scale; the Windows
// adapter keeps one per monitor to convert between Viva's logical
// coordinates and Win32's physical ones.
type monitorGeometry struct {
	Physical Rect // physical pixels
	Scale    float64
}

// logical is the display's bounds in the logical pixels Viva uses on Windows:
// every coordinate divided by that display's own scale.
func (m monitorGeometry) logical() Rect {
	s := m.Scale
	if s <= 0 {
		s = 1
	}
	return Rect{X: m.Physical.X / s, Y: m.Physical.Y / s, W: m.Physical.W / s, H: m.Physical.H / s}
}

// toPhysical converts a logical rectangle on this display to physical pixels.
func (m monitorGeometry) toPhysical(r Rect) Rect {
	l := m.logical()
	return Rect{
		X: math.Round(m.Physical.X + (r.X-l.X)*m.Scale),
		Y: math.Round(m.Physical.Y + (r.Y-l.Y)*m.Scale),
		W: math.Round(r.W * m.Scale),
		H: math.Round(r.H * m.Scale),
	}
}

// findMonitor returns the monitor whose logical bounds contain the point.
func findMonitor(monitors []monitorGeometry, x, y float64) (monitorGeometry, bool) {
	for _, m := range monitors {
		if m.logical().Contains(x, y) {
			return m, true
		}
	}
	return monitorGeometry{}, false
}

// monitorForBounds returns the monitor a window with the given logical
// bounds belongs on: the one whose logical bounds are exactly r (an overlay
// covers its display), else the one containing r's centre. Logical bounds of
// displays with different scales can overlap, so the exact match comes first.
func monitorForBounds(monitors []monitorGeometry, r Rect) (monitorGeometry, bool) {
	for _, m := range monitors {
		l := m.logical()
		if math.Abs(l.X-r.X) < 0.5 && math.Abs(l.Y-r.Y) < 0.5 && math.Abs(l.W-r.W) < 0.5 && math.Abs(l.H-r.H) < 0.5 {
			return m, true
		}
	}
	return findMonitor(monitors, r.X+r.W/2, r.Y+r.H/2)
}

// findMonitorPhysical returns the monitor whose physical bounds contain the point.
func findMonitorPhysical(monitors []monitorGeometry, x, y float64) (monitorGeometry, bool) {
	for _, m := range monitors {
		if m.Physical.Contains(x, y) {
			return m, true
		}
	}
	return monitorGeometry{}, false
}

// windowAt returns the first (front-most) window rectangle containing the point.
func windowAt(windows []Rect, x, y float64) (Rect, bool) {
	for _, w := range windows {
		if w.Contains(x, y) {
			return w, true
		}
	}
	return Rect{}, false
}
