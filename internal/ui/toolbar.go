package ui

import (
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
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
	ActUndo
	ActRedo
	ActPin
	ActSave
	ActCancel
	ActDone
)

type button struct {
	act   Action
	id    vui.ID
	icon  Icon
	label string
	group int
}

var buttons = [...]button{
	{ActRect, Tree.ID(ToolbarScope, "rect"), IconRect, "矩形", 0},
	{ActEllipse, Tree.ID(ToolbarScope, "ellipse"), IconEllipse, "椭圆", 0},
	{ActArrow, Tree.ID(ToolbarScope, "arrow"), IconArrow, "箭头", 0},
	{ActPen, Tree.ID(ToolbarScope, "pen"), IconPen, "画笔", 0},
	{ActUndo, Tree.ID(ToolbarScope, "undo"), IconUndo, "撤销", 1},
	{ActRedo, Tree.ID(ToolbarScope, "redo"), IconRedo, "重做", 1},
	{ActPin, Tree.ID(ToolbarScope, "pin"), IconPin, "钉住", 2},
	{ActSave, Tree.ID(ToolbarScope, "save"), IconSave, "保存", 3},
	{ActCancel, Tree.ID(ToolbarScope, "cancel"), IconCancel, "取消", 3},
	{ActDone, Tree.ID(ToolbarScope, "done"), IconDone, "完成", 3},
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
	case ActRect, ActEllipse, ActArrow, ActPen, ActUndo, ActRedo:
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
	canvas.HitWithOptions(toolbarBack, bounds, "工具栏", vui.HitOptions{SkipFocus: true})
	hovered, focused := canvas.Hovered(), canvas.FocusVisible()
	spec.layout(func(b button, r vui.Rect, divider float32) {
		r = r.Offset(origin.X, origin.Y)
		if divider >= 0 {
			canvas.Fill(vui.Rect{X: origin.X + divider, Y: origin.Y + (ToolbarHeight-20)/2, Width: 1, Height: 20}, t.SurfaceBorder)
		}
		disabled := (b.act == ActUndo && !spec.CanUndo) || (b.act == ActRedo && !spec.CanRedo)
		selected := ActionTool(b.act) != annotate.ToolNone && ActionTool(b.act) == spec.Tool
		canvas.HitWithOptions(b.id, r, b.label, vui.HitOptions{Disabled: disabled, Selected: selected, Hint: b.label})
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

// PropsSize is the property bar's size.
func PropsSize() vui.Size {
	w := 10 + float32(len(annotate.Palette))*SwatchSize + float32(len(annotate.Palette)-1)*8 + 10 + 1 + 6 + float32(len(annotate.Widths))*24 + 6
	return vui.Size{Width: w, Height: PropsHeight}
}

// widthDots are the dot diameters that stand for the three widths.
var widthDots = [...]float32{4, 7, 11}

// DrawProps paints the colour and width bar at origin.
func DrawProps(canvas *vui.Canvas, t Tokens, color annotate.Color, width float32, origin vui.Point) vui.Rect {
	size := PropsSize()
	bounds := vui.Rect{X: origin.X, Y: origin.Y, Width: size.Width, Height: size.Height}
	canvas.Elevate(bounds, RadiusSurface, vui.ShadowMD)
	canvas.RoundedBorder(bounds, RadiusSurface, 1, t.SurfaceBorder, t.Surface)
	canvas.HitWithOptions(propsBack, bounds, "属性", vui.HitOptions{SkipFocus: true})
	x := origin.X + 10
	cy := origin.Y + PropsHeight/2
	for i, c := range annotate.Palette {
		r := vui.Rect{X: x, Y: cy - SwatchSize/2, Width: SwatchSize, Height: SwatchSize}
		id := colorScope.At(i)
		canvas.HitWithOptions(id, r.Inset(vui.All(-3)), colorNames[i], vui.HitOptions{Selected: c == color})
		if c == color {
			canvas.RoundedBorder(r.Inset(vui.All(-3)), SwatchSize/2+3, 2, t.Accent, vui.Color{})
		}
		canvas.RoundedBorder(r, SwatchSize/2, 1, t.SurfaceBorder, annotate.ToColor(c))
		x += SwatchSize + 8
	}
	x += 2
	canvas.Fill(vui.Rect{X: x, Y: cy - 10, Width: 1, Height: 20}, t.SurfaceBorder)
	x += 7
	for i, w := range annotate.Widths {
		cell := vui.Rect{X: x, Y: cy - 12, Width: 24, Height: 24}
		id := widthScope.At(i)
		canvas.HitWithOptions(id, cell, widthNames[i], vui.HitOptions{Selected: w == width})
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
	colorNames = [...]string{"红", "黄", "绿", "蓝", "黑", "白"}
	widthNames = [...]string{"细", "中", "粗"}
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
