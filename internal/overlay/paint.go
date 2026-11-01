package overlay

import (
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/annotate"
	"snaplite/internal/i18n"
	sui "snaplite/internal/ui"
)

// white is the handle fill; the overlay's chrome does not follow the theme.
var white = vui.RGB24(0xFFFFFF)

// paint draws overlay i (docs/05 §6.3). It only reads state, except for
// toolbar clicks, which are deferred with AfterPaint, and the first-paint
// timestamp of the hint capsule.
func (c *Controller) paint(i int, canvas *vui.Canvas, size vui.Size) {
	st := c.States[i]
	t := c.tokens
	bounds := vui.Rect{Width: size.Width, Height: size.Height}

	// 1. The frozen picture, one image pixel per device pixel. It must be the
	// backdrop: Viva paints a layer's images after its rectangles, so a plain
	// DrawBitmap would cover the mask, the selection frame and the toolbar.
	bm := st.Frame.Bitmap
	canvas.DrawBackdrop(bm, vui.Rect{Width: float32(bm.Width), Height: float32(bm.Height)}, bounds)
	cursor := vui.PointerCursorCrosshair
	canvas.HitWithOptions(surfaceID, bounds, i18n.T("截图区域", "Capture area"), vui.HitOptions{Cursor: cursor, SkipFocus: true})

	// 2. The mask, as the four rectangles around the hole.
	hole, hasHole := st.Sel, !empty(st.Sel) && st.Phase != PhaseIdle
	if st.Phase == PhaseIdle && st.HasSnap {
		hole, hasHole = st.Snap, true
	}
	if hasHole {
		fillAround(canvas, bounds, hole, t.Mask)
	} else {
		canvas.Fill(bounds, t.Mask)
	}

	if st.Phase != PhaseIdle && !empty(st.Sel) {
		// 3. Annotations, clipped to the selection.
		annotate.Draw(canvas, &st.Doc, st.Sel, st.view())
		// 4. Frame, handles and size label.
		c.paintSelection(canvas, st, t)
	}
	if st.Phase == PhaseIdle && st.HasSnap {
		strokeRect(canvas, st.Snap, 2, t.Accent)
	}

	// 5. Toolbar and property bar.
	if st.Phase == PhaseEditing && st.Drag == DragNone && !c.finished {
		c.paintToolbar(canvas, st, t, size)
	}

	// 6. Crosshair, magnifier, hint.
	if st.Phase == PhaseIdle && st.HasHover && !st.HasSnap && c.active < 0 {
		canvas.Fill(vui.Rect{X: 0, Y: st.Hover.Y, Width: size.Width, Height: 1}, t.Guide)
		canvas.Fill(vui.Rect{X: st.Hover.X, Y: 0, Width: 1, Height: size.Height}, t.Guide)
	}
	if st.Phase != PhaseEditing && st.HasHover && (c.active < 0 || c.active == i) {
		c.paintMagnifier(canvas, st, t, size)
	}
	if st.Phase == PhaseIdle && c.active < 0 {
		c.paintHint(canvas, st, t, size)
	}
}

func fillAround(canvas *vui.Canvas, b, hole vui.Rect, color vui.Color) {
	canvas.Fill(vui.Rect{X: 0, Y: 0, Width: b.Width, Height: hole.Y}, color)
	canvas.Fill(vui.Rect{X: 0, Y: hole.Y + hole.Height, Width: b.Width, Height: b.Height - hole.Y - hole.Height}, color)
	canvas.Fill(vui.Rect{X: 0, Y: hole.Y, Width: hole.X, Height: hole.Height}, color)
	canvas.Fill(vui.Rect{X: hole.X + hole.Width, Y: hole.Y, Width: b.Width - hole.X - hole.Width, Height: hole.Height}, color)
}

// strokeRect draws a w-wide frame just outside r.
func strokeRect(canvas *vui.Canvas, r vui.Rect, w float32, color vui.Color) {
	canvas.Fill(vui.Rect{X: r.X - w, Y: r.Y - w, Width: r.Width + 2*w, Height: w}, color)
	canvas.Fill(vui.Rect{X: r.X - w, Y: r.Y + r.Height, Width: r.Width + 2*w, Height: w}, color)
	canvas.Fill(vui.Rect{X: r.X - w, Y: r.Y, Width: w, Height: r.Height}, color)
	canvas.Fill(vui.Rect{X: r.X + r.Width, Y: r.Y, Width: w, Height: r.Height}, color)
}

func (c *Controller) paintSelection(canvas *vui.Canvas, st *State, t sui.Tokens) {
	sel := st.Sel
	selCursor := vui.PointerCursorDefault
	if st.Doc.Tool != annotate.ToolNone {
		selCursor = vui.PointerCursorCrosshair
	}
	if st.Phase == PhaseEditing {
		canvas.HitWithOptions(selectionID, sel, i18n.T("选区", "Selection"), vui.HitOptions{Cursor: selCursor, SkipFocus: true})
	}
	strokeRect(canvas, sel, 1, t.Accent)
	for h, p := range handlePoints(sel) {
		r := vui.Rect{X: p.X - sui.HandleSize/2, Y: p.Y - sui.HandleSize/2, Width: sui.HandleSize, Height: sui.HandleSize}
		canvas.Border(r, 1, t.Accent, white)
		if st.Phase == PhaseEditing {
			canvas.HitWithOptions(handleScope.At(h), handleRect(p), i18n.T("调整选区", "Resize selection"), vui.HitOptions{Cursor: handleCursor(h), SkipFocus: true})
		}
	}
	style := vui.TextStyle{Size: sui.LabelTextSize, Weight: vui.WeightMedium, Color: white}
	w := canvas.TextWidthWeight(st.label, style.Size, style.Weight) + 12
	lr := labelRect(sel, w, 22)
	canvas.Rounded(lr, sui.RadiusSwatch, t.LabelBg)
	canvas.LabelCenter(st.label, lr, style)
}

func (c *Controller) paintToolbar(canvas *vui.Canvas, st *State, t sui.Tokens, size vui.Size) {
	spec := c.toolbarSpec(st)
	bar := sui.ToolbarSize(canvas, spec)
	extra := float32(0)
	if st.Doc.Tool != annotate.ToolNone {
		extra = sui.PropsGap + sui.PropsHeight
	}
	r, place := PlaceToolbar(st.Sel, bar, size, extra)
	sui.DrawToolbar(canvas, t, spec, vui.Point{X: r.X, Y: r.Y})
	if a := sui.ToolbarClick(canvas); a != sui.ActNone {
		canvas.AfterPaint(func() { c.act(st, a) })
	}
	if st.Doc.Tool == annotate.ToolNone {
		return
	}
	colors := st.Doc.Tool != annotate.ToolMosaic
	props := sui.PropsSize(colors)
	px := min(max(r.X+r.Width-props.Width, 0), max(size.Width-props.Width, 0))
	py := r.Y + r.Height + sui.PropsGap
	if place == PlaceAbove {
		py = r.Y - sui.PropsGap - props.Height
	}
	sui.DrawProps(canvas, t, st.Doc.Color, st.Doc.Width, vui.Point{X: px, Y: py}, colors)
	if ci, wi := sui.PropsClick(canvas); ci >= 0 || wi >= 0 {
		canvas.AfterPaint(func() {
			if ci >= 0 {
				st.Doc.Color = annotate.Palette[ci]
			}
			if wi >= 0 {
				st.Doc.Width = annotate.Widths[wi]
			}
		})
	}
}

// Magnifier geometry (docs/01 §2.2): 15×15 pixels at 8×.
const (
	magPixels = 15
	magCell   = 8
	magSide   = magPixels * magCell
	magInfo   = 26
)

func (c *Controller) paintMagnifier(canvas *vui.Canvas, st *State, t sui.Tokens, size vui.Size) {
	img := st.Frame.Image
	center := st.Frame.ToImage(st.Hover)
	box := vui.Rect{X: st.Hover.X + 20, Y: st.Hover.Y + 20, Width: magSide + 4, Height: magSide + 4 + magInfo}
	if box.X+box.Width > size.Width {
		box.X = st.Hover.X - 20 - box.Width
	}
	if box.Y+box.Height > size.Height {
		box.Y = st.Hover.Y - 20 - box.Height
	}
	canvas.Rounded(box, sui.RadiusSwatch, t.LabelBg)
	grid := vui.Rect{X: box.X + 2, Y: box.Y + 2, Width: magSide, Height: magSide}
	bounds := img.Bounds()
	for gy := 0; gy < magPixels; gy++ {
		for gx := 0; gx < magPixels; gx++ {
			x, y := center.X+gx-magPixels/2, center.Y+gy-magPixels/2
			color := vui.RGB(0, 0, 0)
			if x >= bounds.Min.X && y >= bounds.Min.Y && x < bounds.Max.X && y < bounds.Max.Y {
				p := img.Pix[img.PixOffset(x, y):]
				color = vui.RGB(p[0], p[1], p[2])
			}
			canvas.Fill(vui.Rect{X: grid.X + float32(gx*magCell), Y: grid.Y + float32(gy*magCell), Width: magCell, Height: magCell}, color)
		}
	}
	mid := float32(magPixels / 2 * magCell)
	strokeRect(canvas, vui.Rect{X: grid.X + mid, Y: grid.Y + mid, Width: magCell, Height: magCell}, 1, white)
	info := vui.Rect{X: box.X, Y: grid.Y + magSide + 4, Width: box.Width, Height: magInfo - 6}
	canvas.LabelCenter(st.pixel, info, vui.TextStyle{Size: sui.LabelTextSize, Weight: vui.WeightMedium, Color: white, Mono: true})
}

func (c *Controller) paintHint(canvas *vui.Canvas, st *State, t sui.Tokens, size vui.Size) {
	now := canvas.Now()
	if !st.hintStarted {
		// The one write a paint makes: when the capsule first appeared, on
		// the canvas's own clock, so it can fade without a timer.
		st.hintStart, st.hintStarted = now, true
	}
	elapsed := now - st.hintStart
	alpha := float32(1)
	switch {
	case elapsed < hintShown:
		canvas.RequestFrameIn(hintShown - elapsed)
	case elapsed < hintShown+hintFade && !canvas.ReduceMotion():
		alpha = 1 - float32(elapsed-hintShown)/float32(hintFade)
		canvas.RequestFrame()
	default:
		return
	}
	style := vui.TextStyle{Size: sui.ControlTextSz, Weight: vui.WeightMedium, Color: white}
	style.Color.Alpha = alpha
	w := canvas.TextWidthWeight(HintRegion(), style.Size, style.Weight) + 32
	r := vui.Rect{X: (size.Width - w) / 2, Y: 24, Width: w, Height: 34}
	bg := t.LabelBg
	bg.Alpha *= alpha
	canvas.Rounded(r, r.Height/2, bg)
	canvas.LabelCenter(HintRegion(), r, style)
}
