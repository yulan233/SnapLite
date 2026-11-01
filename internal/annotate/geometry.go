package annotate

import (
	"image"
	"math"
)

// FPoint is a point in image pixels with sub-pixel precision. The screen
// painter and the rasterizer share the geometry in this file, so what is
// drawn on screen is what ends up in the PNG.
type FPoint struct{ X, Y float64 }

func fp(p image.Point) FPoint { return FPoint{X: float64(p.X), Y: float64(p.Y)} }

func mid(a, b FPoint) FPoint { return FPoint{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2} }

// ArrowGeometry is an arrow's shaft and solid head.
type ArrowGeometry struct {
	Start, ShaftEnd FPoint
	Head            [3]FPoint // tip, then the two base corners
}

// Arrow computes the arrow from start to end for a stroke width w: the head
// is max(10, 4w) long and max(5, 2w) half-wide, and the shaft stops at the
// head's base so a translucent colour would not double up.
func Arrow(start, end image.Point, w float64) ArrowGeometry {
	a, b := fp(start), fp(end)
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(dx, dy)
	g := ArrowGeometry{Start: a, ShaftEnd: b, Head: [3]FPoint{b, b, b}}
	if length == 0 {
		return g
	}
	ux, uy := dx/length, dy/length
	headLen := math.Max(10, 4*w)
	half := math.Max(5, 2*w)
	if headLen > length {
		scale := length / headLen
		headLen, half = length, half*scale
	}
	base := FPoint{X: b.X - ux*headLen, Y: b.Y - uy*headLen}
	nx, ny := -uy, ux
	g.ShaftEnd = base
	g.Head = [3]FPoint{b, {X: base.X + nx*half, Y: base.Y + ny*half}, {X: base.X - nx*half, Y: base.Y - ny*half}}
	return g
}

// Box is the normalised rectangle spanned by two points.
func Box(a, b image.Point) (x0, y0, x1, y1 float64) {
	x0, x1 = float64(min(a.X, b.X)), float64(max(a.X, b.X))
	y0, y1 = float64(min(a.Y, b.Y)), float64(max(a.Y, b.Y))
	return
}

// EllipsePolyline samples the ellipse inscribed in the box of a and b: a
// segment every 2 pixels of circumference, at least 64. The result is closed
// (the last point equals the first).
func EllipsePolyline(a, b image.Point) []FPoint {
	x0, y0, x1, y1 := Box(a, b)
	cx, cy := (x0+x1)/2, (y0+y1)/2
	rx, ry := (x1-x0)/2, (y1-y0)/2
	// Ramanujan's approximation of the circumference.
	h := math.Pow(rx-ry, 2) / math.Max(math.Pow(rx+ry, 2), 1e-9)
	circumference := math.Pi * (rx + ry) * (1 + 3*h/(10+math.Sqrt(4-3*h)))
	n := max(64, int(circumference/2))
	out := make([]FPoint, n+1)
	for i := 0; i < n; i++ {
		t := 2 * math.Pi * float64(i) / float64(n)
		out[i] = FPoint{X: cx + rx*math.Cos(t), Y: cy + ry*math.Sin(t)}
	}
	out[n] = out[0]
	return out
}

// RectPolyline is the closed outline of the box of a and b.
func RectPolyline(a, b image.Point) []FPoint {
	x0, y0, x1, y1 := Box(a, b)
	return []FPoint{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0, y0}}
}

// PenPolyline flattens a pen stroke: the points are joined by quadratic
// Béziers through their midpoints (the same curve the screen path uses),
// subdivided into about 2-pixel segments.
func PenPolyline(points []image.Point) []FPoint {
	if len(points) == 0 {
		return nil
	}
	if len(points) < 3 {
		out := make([]FPoint, len(points))
		for i, p := range points {
			out[i] = fp(p)
		}
		return out
	}
	out := []FPoint{fp(points[0])}
	from := fp(points[0])
	for i := 1; i < len(points)-1; i++ {
		control := fp(points[i])
		to := mid(control, fp(points[i+1]))
		length := math.Hypot(control.X-from.X, control.Y-from.Y) + math.Hypot(to.X-control.X, to.Y-control.Y)
		steps := max(1, int(length/2))
		for s := 1; s <= steps; s++ {
			t := float64(s) / float64(steps)
			u := 1 - t
			out = append(out, FPoint{
				X: u*u*from.X + 2*u*t*control.X + t*t*to.X,
				Y: u*u*from.Y + 2*u*t*control.Y + t*t*to.Y,
			})
		}
		from = to
	}
	out = append(out, fp(points[len(points)-1]))
	return out
}

// Constrain applies the Shift modifier: a square box for rectangles and
// ellipses, a 45° step for arrows.
func Constrain(tool Tool, start, end image.Point) image.Point {
	dx, dy := end.X-start.X, end.Y-start.Y
	switch tool {
	case ToolRect, ToolEllipse:
		side := max(abs(dx), abs(dy))
		return image.Point{X: start.X + sign(dx)*side, Y: start.Y + sign(dy)*side}
	case ToolArrow:
		length := math.Hypot(float64(dx), float64(dy))
		angle := math.Round(math.Atan2(float64(dy), float64(dx))/(math.Pi/4)) * (math.Pi / 4)
		return image.Point{
			X: start.X + int(math.Round(length*math.Cos(angle))),
			Y: start.Y + int(math.Round(length*math.Sin(angle))),
		}
	}
	return end
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}
