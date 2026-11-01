package ui

import (
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/i18n"
)

// Tree is the identity tree every SnapLite window paints in. It is used only
// on the event-loop thread.
var Tree = vui.Tree{}

var (
	toolbarFeature = Tree.Feature("toolbar")
	// ToolbarScope contains every toolbar and property-bar control, so input
	// handlers can tell a press on the bar from a press on the page.
	ToolbarScope = Tree.Sub(toolbarFeature, Tree.Name("buttons"))
	colorScope   = Tree.Sub(ToolbarScope, Tree.Name("colors"))
	widthScope   = Tree.Sub(ToolbarScope, Tree.Name("widths"))
	toolbarBack  = Tree.ID(ToolbarScope, "surface")
	propsBack    = Tree.ID(ToolbarScope, "props")
)

// Action is a toolbar button.
type Action uint8

const (
	ActNone Action = iota
	ActRect
	ActEllipse
	ActArrow
	ActPen
	ActMosaic
	ActUndo
	ActRedo
	ActPin
	ActSave
	ActCancel
	ActDone
)

type button struct {
	act    Action
	id     vui.ID
	icon   Icon
	zh, en string // the label
	group  int
}

func (b button) label() string { return i18n.T(b.zh, b.en) }

var buttons = [...]button{
	{ActRect, Tree.ID(ToolbarScope, "rect"), IconRect, "矩形", "Rectangle", 0},
	{ActEllipse, Tree.ID(ToolbarScope, "ellipse"), IconEllipse, "椭圆", "Ellipse", 0},
	{ActArrow, Tree.ID(ToolbarScope, "arrow"), IconArrow, "箭头", "Arrow", 0},
	{ActPen, Tree.ID(ToolbarScope, "pen"), IconPen, "画笔", "Pen", 0},
	{ActMosaic, Tree.ID(ToolbarScope, "mosaic"), IconMosaic, "马赛克", "Mosaic", 0},
	{ActUndo, Tree.ID(ToolbarScope, "undo"), IconUndo, "撤销", "Undo", 1},
	{ActRedo, Tree.ID(ToolbarScope, "redo"), IconRedo, "重做", "Redo", 1},
	{ActPin, Tree.ID(ToolbarScope, "pin"), IconPin, "钉住", "Pin", 2},
	{ActSave, Tree.ID(ToolbarScope, "save"), IconSave, "保存", "Save", 3},
	{ActCancel, Tree.ID(ToolbarScope, "cancel"), IconCancel, "取消", "Cancel", 3},
	{ActDone, Tree.ID(ToolbarScope, "done"), IconDone, "完成", "Done", 3},
}

// ButtonID returns the identity of an action's button.
func ButtonID(a Action) vui.ID {
	for _, b := range buttons {
		if b.act == a {
			return b.id
		}
	}
	return vui.ID{}
}

// ToolAction maps a tool to its button and back.
func ToolAction(t annotate.Tool) Action {
	switch t {
	case annotate.ToolRect:
		return ActRect
	case annotate.ToolEllipse:
		return ActEllipse
	case annotate.ToolArrow:
		return ActArrow
	case annotate.ToolPen:
		return ActPen
	case annotate.ToolMosaic:
		return ActMosaic
	}
	return ActNone
}

// ActionTool is the tool a tool button selects.
func ActionTool(a Action) annotate.Tool {
	switch a {
	case ActRect:
		return annotate.ToolRect
	case ActEllipse:
		return annotate.ToolEllipse
	case ActArrow:
		return annotate.ToolArrow
	case ActPen:
		return annotate.ToolPen
	case ActMosaic:
		return annotate.ToolMosaic
	}
	return annotate.ToolNone
}

// ToolbarSpec says which buttons a toolbar shows and their state.
type ToolbarSpec struct {
	Tools   bool // the annotation and undo groups
	Pin     bool
	Save    bool
	Cancel  bool
	Done    bool
	Tool    annotate.Tool
	CanUndo bool
	CanRedo bool
}

func (s ToolbarSpec) shows(a Action) bool {
	switch a {
	case ActRect, ActEllipse, ActArrow, ActPen, ActMosaic, ActUndo, ActRedo:
		return s.Tools
	case ActPin:
		return s.Pin
	case ActSave:
		return s.Save
	case ActCancel:
		return s.Cancel
	case ActDone:
		return s.Done
	}
	return false
}

// layout calls fn for each visible button with its rectangle relative to the
// toolbar's origin and returns the toolbar's width.
func (s ToolbarSpec) layout(fn func(b button, r vui.Rect, dividerX float32)) float32 {
	x := float32(ToolbarPadding)
	group := -1
	y := float32(ToolbarHeight-ButtonSize) / 2
	for _, b := range buttons {
		if !s.shows(b.act) {
			continue
		}
		divider := float32(-1)
		if group >= 0 && b.group != group {
			x += GroupGap / 2
			divider = x
			x += 1 + GroupGap/2
		} else if group >= 0 {
			x += ButtonGap
		}
		group = b.group
		if fn != nil {
			fn(b, vui.Rect{X: x, Y: y, Width: ButtonSize, Height: ButtonSize}, divider)
		}
		x += ButtonSize
	}
	return x + ToolbarPadding
}

// ToolbarSize is the toolbar's size for spec.
func ToolbarSize(canvas *vui.Canvas, spec ToolbarSpec) vui.Size {
	return vui.Size{Width: spec.layout(nil), Height: ToolbarHeight}
}

// DrawToolbar paints the toolbar with its top-left corner at origin and
// declares a hit per button. It returns the toolbar's rectangle.
func DrawToolbar(canvas *vui.Canvas, t Tokens, spec ToolbarSpec, origin vui.Point) vui.Rect {
	size := ToolbarSize(canvas, spec)
	bounds := vui.Rect{X: origin.X, Y: origin.Y, Width: size.Width, Height: size.Height}
	canvas.Elevate(bounds, RadiusSurface, vui.ShadowMD)
	canvas.RoundedBorder(bounds, RadiusSurface, 1, t.SurfaceBorder, t.Surface)
	// The surface itself blocks presses so they do not reach the page.
	canvas.HitWithOptions(toolbarBack, bounds, i18n.T("工具栏", "Toolbar"), vui.HitOptions{SkipFocus: true})
	hovered, focused := canvas.Hovered(), canvas.FocusVisible()
	spec.layout(func(b button, r vui.Rect, divider float32) {
		r = r.Offset(origin.X, origin.Y)
		if divider >= 0 {
			canvas.Fill(vui.Rect{X: origin.X + divider, Y: origin.Y + (ToolbarHeight-20)/2, Width: 1, Height: 20}, t.SurfaceBorder)
		}
		disabled := (b.act == ActUndo && !spec.CanUndo) || (b.act == ActRedo && !spec.CanRedo)
		selected := ActionTool(b.act) != annotate.ToolNone && ActionTool(b.act) == spec.Tool
		label := b.label()
		canvas.HitWithOptions(b.id, r, label, vui.HitOptions{Disabled: disabled, Selected: selected, Hint: label})
		fg := t.Text
		switch {
		case b.act == ActDone:
			bg := t.Accent
			if hovered == b.id {
				bg = t.Accent.Mix(t.OnAccent, 0.12)
			}
			canvas.Rounded(r, RadiusButton, bg)
			fg = t.OnAccent
		case selected:
			canvas.Rounded(r, RadiusButton, t.Selected)
			fg = t.Accent
		case hovered == b.id && !disabled:
			canvas.Rounded(r, RadiusButton, t.Hover)
		}
		if b.act == ActCancel && hovered == b.id {
			fg = t.Danger
		}
		if disabled {
			fg = t.TextMuted.Mix(t.Surface, 0.4)
		}
		if focused == b.id {
			canvas.RoundedBorder(r.Inset(vui.All(-2)), RadiusButton+2, 2, t.Accent, vui.Color{})
		}
		DrawIcon(canvas, b.icon, r, fg)
	})
	return bounds
}

// ToolbarClick reports which toolbar button this paint's click went to.
func ToolbarClick(canvas *vui.Canvas) Action {
	for _, b := range buttons {
		if canvas.Clicked(b.id) {
			return b.act
		}
	}
	return ActNone
}

// IsToolbar reports whether a hit identity belongs to a toolbar or property bar.
func IsToolbar(id vui.ID) bool {
	return !id.IsZero() && Tree.Within(id.Scope(), ToolbarScope)
}

// PropsSize is the property bar's size; without colours (the mosaic has
// none) it holds only the widths.
func PropsSize(colors bool) vui.Size {
	w := 6 + float32(len(annotate.Widths))*24 + 6
	if colors {
		// The swatches, the divider and its gaps come before the widths.
		w += 10 + float32(len(annotate.Palette))*SwatchSize + float32(len(annotate.Palette)-1)*8 + 10 + 1
	}
	return vui.Size{Width: w, Height: PropsHeight}
}

// widthDots are the dot diameters that stand for the three widths.
var widthDots = [...]float32{4, 7, 11}

// DrawProps paints the colour and width bar at origin, or only the widths
// when colors is false.
func DrawProps(canvas *vui.Canvas, t Tokens, color annotate.Color, width float32, origin vui.Point, colors bool) vui.Rect {
	size := PropsSize(colors)
	bounds := vui.Rect{X: origin.X, Y: origin.Y, Width: size.Width, Height: size.Height}
	canvas.Elevate(bounds, RadiusSurface, vui.ShadowMD)
	canvas.RoundedBorder(bounds, RadiusSurface, 1, t.SurfaceBorder, t.Surface)
	canvas.HitWithOptions(propsBack, bounds, i18n.T("属性", "Properties"), vui.HitOptions{SkipFocus: true})
	x := origin.X + 10
	cy := origin.Y + PropsHeight/2
	for i, c := range annotate.Palette {
		if !colors {
			break
		}
		r := vui.Rect{X: x, Y: cy - SwatchSize/2, Width: SwatchSize, Height: SwatchSize}
		id := colorScope.At(i)
		canvas.HitWithOptions(id, r.Inset(vui.All(-3)), i18n.T(colorNames[i][0], colorNames[i][1]), vui.HitOptions{Selected: c == color})
		if c == color {
			canvas.RoundedBorder(r.Inset(vui.All(-3)), SwatchSize/2+3, 2, t.Accent, vui.Color{})
		}
		canvas.RoundedBorder(r, SwatchSize/2, 1, t.SurfaceBorder, annotate.ToColor(c))
		x += SwatchSize + 8
	}
	if colors {
		x += 2
		canvas.Fill(vui.Rect{X: x, Y: cy - 10, Width: 1, Height: 20}, t.SurfaceBorder)
		x += 7
	} else {
		x = origin.X + 6
	}
	for i, w := range annotate.Widths {
		cell := vui.Rect{X: x, Y: cy - 12, Width: 24, Height: 24}
		id := widthScope.At(i)
		canvas.HitWithOptions(id, cell, i18n.T(widthNames[i][0], widthNames[i][1]), vui.HitOptions{Selected: w == width})
		if w == width {
			canvas.Rounded(cell, RadiusButton, t.Selected)
		} else if canvas.Hovered() == id {
			canvas.Rounded(cell, RadiusButton, t.Hover)
		}
		d := widthDots[i]
		canvas.Rounded(cell.Centered(d, d), d/2, t.Text)
		x += 24
	}
	return bounds
}

var (
	colorNames = [...][2]string{{"红", "Red"}, {"黄", "Yellow"}, {"绿", "Green"}, {"蓝", "Blue"}, {"黑", "Black"}, {"白", "White"}}
	widthNames = [...][2]string{{"细", "Thin"}, {"中", "Medium"}, {"粗", "Thick"}}
)

// PropsClick reports a click on a colour (index) or a width (index), -1 for none.
func PropsClick(canvas *vui.Canvas) (colorIndex, widthIndex int) {
	colorIndex, widthIndex = -1, -1
	if i, ok := canvas.ClickedIn(colorScope); ok {
		colorIndex = i
	}
	if i, ok := canvas.ClickedIn(widthScope); ok {
		widthIndex = i
	}
	return
}
