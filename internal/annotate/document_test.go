package annotate

import (
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"
)

func pt(x, y int) image.Point { return image.Point{X: x, Y: y} }

func draw(d *Document, tool Tool, from, to image.Point, shift bool) bool {
	d.Tool = tool
	d.Begin(from)
	d.Extend(to, shift)
	return d.Commit()
}

func TestUndoRedo(t *testing.T) {
	d := NewDocument()
	if d.Dirty() || d.CanUndo() {
		t.Fatal("new document")
	}
	if !draw(&d, ToolRect, pt(10, 10), pt(60, 40), false) {
		t.Fatal("rect not committed")
	}
	draw(&d, ToolArrow, pt(0, 0), pt(100, 0), false)
	if len(d.Shapes) != 2 || !d.Dirty() {
		t.Fatal("two shapes")
	}
	if !d.Undo() || len(d.Shapes) != 1 || !d.CanRedo() {
		t.Fatal("undo")
	}
	if !d.Redo() || len(d.Shapes) != 2 || d.Shapes[1].Tool != ToolArrow {
		t.Fatal("redo")
	}
	d.Undo()
	draw(&d, ToolEllipse, pt(0, 0), pt(30, 30), false)
	if d.CanRedo() {
		t.Fatal("a new shape clears the redo stack")
	}
	d.MarkClean()
	if d.Dirty() {
		t.Fatal("clean after MarkClean")
	}
	d.Undo()
	d.Undo()
	d.Undo()
	if d.Undo() || len(d.Shapes) != 0 || len(d.paths) != 0 || !d.Dirty() {
		t.Fatal("undo past empty")
	}
}

func TestTinyShapesAreDropped(t *testing.T) {
	d := NewDocument()
	if draw(&d, ToolRect, pt(5, 5), pt(6, 7), false) {
		t.Fatal("a 1x2 rectangle is too small")
	}
	d.Tool = ToolPen
	d.Begin(pt(0, 0))
	d.Extend(pt(1, 1), false)
	if d.Commit() {
		t.Fatal("a pen dot is too small")
	}
	if d.CancelDraft() {
		t.Fatal("no draft after commit")
	}
	d.Tool = ToolNone
	d.Begin(pt(0, 0))
	if d.Draft != nil {
		t.Fatal("no draft without a tool")
	}
}

func TestShiftConstraints(t *testing.T) {
	d := NewDocument()
	draw(&d, ToolRect, pt(10, 10), pt(50, 20), true)
	if p := d.Shapes[0].Points[1]; p != pt(50, 50) {
		t.Fatalf("square end = %v", p)
	}
	draw(&d, ToolEllipse, pt(10, 10), pt(-20, 0), true)
	if p := d.Shapes[1].Points[1]; p != pt(-20, -20) {
		t.Fatalf("circle end = %v", p)
	}
	draw(&d, ToolArrow, pt(0, 0), pt(100, 10), true)
	if p := d.Shapes[2].Points[1]; p.Y != 0 || p.X < 99 || p.X > 101 {
		t.Fatalf("horizontal snap = %v", p)
	}
	draw(&d, ToolArrow, pt(0, 0), pt(100, 90), true)
	if p := d.Shapes[3].Points[1]; p.X != p.Y {
		t.Fatalf("45° snap = %v", p)
	}
}

func TestPenSmoothingDropsClosePoints(t *testing.T) {
	d := NewDocument()
	d.Tool = ToolPen
	d.Begin(pt(0, 0))
	for x := 1; x <= 40; x++ {
		d.Extend(pt(x, 0), false)
	}
	if !d.Commit() {
		t.Fatal("pen stroke")
	}
	pts := d.Shapes[0].Points
	for i := 1; i < len(pts); i++ {
		if pts[i].X-pts[i-1].X < 2 {
			t.Fatalf("samples closer than 2px kept: %v", pts)
		}
	}
	poly := PenPolyline(pts)
	if poly[0] != (FPoint{}) || poly[len(poly)-1] != (FPoint{X: float64(pts[len(pts)-1].X)}) {
		t.Fatal("pen polyline must start and end on the samples")
	}
}

func TestArrowGeometry(t *testing.T) {
	g := Arrow(pt(0, 0), pt(100, 0), 4)
	if g.ShaftEnd.X != 84 || g.Head[0].X != 100 { // head length max(10, 16) = 16
		t.Fatalf("arrow = %+v", g)
	}
	if math.Abs(g.Head[1].Y-8) > 1e-9 || math.Abs(g.Head[2].Y+8) > 1e-9 { // half width max(5, 8)
		t.Fatalf("head corners = %+v", g.Head)
	}
	thin := Arrow(pt(0, 0), pt(0, 100), 1)
	if thin.ShaftEnd.Y != 90 {
		t.Fatalf("minimum head length is 10: %+v", thin)
	}
	short := Arrow(pt(0, 0), pt(5, 0), 4)
	if short.ShaftEnd.X != 0 {
		t.Fatalf("a short arrow is all head: %+v", short)
	}
}

func TestEllipsePolyline(t *testing.T) {
	poly := EllipsePolyline(pt(0, 0), pt(200, 100))
	if len(poly) < 65 {
		t.Fatalf("too few segments: %d", len(poly))
	}
	if poly[0] != poly[len(poly)-1] {
		t.Fatal("not closed")
	}
	for _, p := range poly {
		v := math.Pow((p.X-100)/100, 2) + math.Pow((p.Y-50)/50, 2)
		if math.Abs(v-1) > 1e-9 {
			t.Fatalf("point off the ellipse: %+v", p)
		}
	}
}

func TestViewMapping(t *testing.T) {
	v := View{Origin: vui.Point{X: 10, Y: 20}, Zoom: 0.5}
	w := v.ToWindow(pt(100, 40))
	if w != (vui.Point{X: 60, Y: 40}) {
		t.Fatalf("ToWindow = %v", w)
	}
	if p := v.ToImage(w); p != pt(100, 40) {
		t.Fatalf("ToImage = %v", p)
	}
}

// TestDrawMapsImageCoordinates paints a committed rectangle through a real
// canvas in a Driver and checks the path lands where the view puts it.
func TestDrawMapsImageCoordinates(t *testing.T) {
	d := NewDocument()
	draw(&d, ToolRect, pt(100, 100), pt(300, 200), false)
	draw(&d, ToolArrow, pt(0, 0), pt(200, 0), false)
	d.Tool = ToolPen
	d.Begin(pt(10, 10))
	d.Extend(pt(50, 50), false)
	view := View{Origin: vui.Point{X: 20, Y: 10}, Zoom: 0.5}
	config := app.Config{Width: 400, Height: 300, Paint: func(canvas *vui.Canvas, size vui.Size) {
		Draw(canvas, &d, vui.Rect{Width: size.Width, Height: size.Height}, view)
	}}
	var canvas vui.Canvas
	driver, err := app.NewDriver(config, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	tris := canvas.Triangles()
	if len(tris) == 0 {
		t.Fatal("nothing drawn")
	}
	// Every triangle lies inside the rectangle's window bounds (70..170, 60..110)
	// padded by the stroke, or near the arrow and the pen draft.
	var minX, minY, maxX, maxY float32 = 1e9, 1e9, -1e9, -1e9
	for _, tri := range tris {
		for _, p := range tri.Points {
			minX, minY = min(minX, p.X), min(minY, p.Y)
			maxX, maxY = max(maxX, p.X), max(maxY, p.Y)
		}
	}
	if maxX < 169 || maxX > 175 || maxY < 109 || maxY > 115 {
		t.Fatalf("rectangle not mapped by the view: max %v,%v", maxX, maxY)
	}
	if minX > 21 || minY > 11 {
		t.Fatalf("arrow start not at the view origin: min %v,%v", minX, minY)
	}
}
