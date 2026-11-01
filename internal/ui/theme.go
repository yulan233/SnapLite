package ui

import (
	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"
)

// Tokens are SnapLite's design tokens (docs/02 §2.1). Interface code uses
// these, never literal colours.
type Tokens struct {
	Dark          bool
	Accent        vui.Color
	Mask          vui.Color
	Surface       vui.Color
	SurfaceBorder vui.Color
	Text          vui.Color
	TextMuted     vui.Color
	Hover         vui.Color
	Selected      vui.Color
	Danger        vui.Color
	LabelBg       vui.Color
	// OnAccent is the foreground on an Accent fill.
	OnAccent vui.Color
	// Guide is the crosshair colour on the overlay.
	Guide vui.Color
}

func withAlpha(c vui.Color, a float32) vui.Color {
	c.Alpha = a
	return c
}

// Light returns the light-appearance tokens.
func Light() Tokens {
	accent := vui.RGB24(0x2E7CF6)
	return Tokens{
		Accent:        accent,
		Mask:          vui.RGBA(0, 0, 0, 0.40),
		Surface:       vui.RGB24(0xFFFFFF),
		SurfaceBorder: vui.RGBA(0, 0, 0, 0.08),
		Text:          vui.RGB24(0x1D1F23),
		TextMuted:     vui.RGB24(0x6B7079),
		Hover:         vui.RGBA(0, 0, 0, 0.06),
		Selected:      withAlpha(accent, 0.14),
		Danger:        vui.RGB24(0xE5484D),
		LabelBg:       vui.RGBA(0, 0, 0, 0.72),
		OnAccent:      vui.RGB24(0xFFFFFF),
		Guide:         vui.RGBA(255, 255, 255, 0.60),
	}
}

// Dark returns the dark-appearance tokens.
func Dark() Tokens {
	accent := vui.RGB24(0x4C8DF8)
	t := Light()
	t.Dark = true
	t.Accent = accent
	t.Surface = vui.RGB24(0x1F2023)
	t.SurfaceBorder = vui.RGBA(255, 255, 255, 0.10)
	t.Text = vui.RGB24(0xECEDEE)
	t.TextMuted = vui.RGB24(0x9A9FA6)
	t.Hover = vui.RGBA(255, 255, 255, 0.08)
	t.Selected = withAlpha(accent, 0.24)
	t.Danger = vui.RGB24(0xF2555A)
	return t
}

// For returns the tokens for a system appearance; unknown is light.
func For(a app.Appearance) Tokens {
	if a == app.AppearanceDark {
		return Dark()
	}
	return Light()
}

// Current returns the tokens for the system's appearance now.
func Current() Tokens { return For(app.SystemAppearance()) }

// VivaTheme is Viva's own theme for flex components, with SnapLite's
// surface and accent colours.
func (t Tokens) VivaTheme() vui.Theme {
	theme := vui.LightTheme()
	if t.Dark {
		theme = vui.DarkTheme()
	}
	theme.Primary = t.Accent
	theme.PrimaryForeground = t.OnAccent
	theme.Ring = t.Accent
	theme.Background = t.Surface
	theme.Popover = t.Surface
	theme.Card = t.Surface
	theme.Foreground = t.Text
	theme.MutedForeground = t.TextMuted
	theme.Hover = t.Hover
	theme.Danger = t.Danger
	return theme
}

// Metrics (docs/02 §2.2), in logical pixels.
const (
	RadiusSurface  = 10
	RadiusButton   = 6
	RadiusSwatch   = 4
	ToolbarHeight  = 40
	ButtonSize     = 32
	IconSize       = 20
	ButtonGap      = 2
	GroupGap       = 8
	FloatGap       = 8
	HandleSize     = 7
	PropsHeight    = 36
	PropsGap       = 6
	SwatchSize     = 18
	LabelTextSize  = 12
	BodyTextSize   = 13
	MutedTextSize  = 12
	ControlTextSz  = 13
	ToolbarPadding = 4
)
