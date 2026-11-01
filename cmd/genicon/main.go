//go:build ignore

// Command genicon draws assets/icon.png (1024×1024 application icon: a
// rounded square in an Accent gradient with a white viewfinder) and
// assets/tray-template.png (32×32 template). Run from the module root:
//
//	go run ./cmd/genicon/main.go
package main

import (
	"image"
	"log"
	"math"
	"os"

	"snaplite/internal/annotate"
	"snaplite/internal/raster"
	sui "snaplite/internal/ui"
)

func main() {
	const size = 1024
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Accent gradient #4C8DF8 → #2E7CF6 → #1F5FD6 from top-left to bottom-right,
	// inside a rounded square (macOS grid: 824 px body, 185 px corner radius).
	const inset, radius = 100.0, 185.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			cov := roundedCoverage(float64(x)+0.5, float64(y)+0.5, inset, size-inset, radius)
			if cov <= 0 {
				continue
			}
			t := (float64(x) + float64(y)) / (2 * size)
			r := lerp(0x4C, 0x1F, t)
			g := lerp(0x8D, 0x5F, t)
			b := lerp(0xF8, 0xD6, t)
			p := img.Pix[img.PixOffset(x, y):]
			p[0], p[1], p[2], p[3] = uint8(r), uint8(g), uint8(b), uint8(cov*255+0.5)
		}
	}
	white := annotate.Color{R: 255, G: 255, B: 255, A: 255}
	line := func(a, b image.Point) annotate.Shape {
		return annotate.Shape{Tool: annotate.ToolPen, Color: white, Width: 56, Points: []image.Point{a, b}}
	}
	lo, hi, arm := 300, 724, 150
	shapes := []annotate.Shape{
		line(image.Pt(lo, lo+arm), image.Pt(lo, lo)), line(image.Pt(lo, lo), image.Pt(lo+arm, lo)),
		line(image.Pt(hi-arm, lo), image.Pt(hi, lo)), line(image.Pt(hi, lo), image.Pt(hi, lo+arm)),
		line(image.Pt(hi, hi-arm), image.Pt(hi, hi)), line(image.Pt(hi, hi), image.Pt(hi-arm, hi)),
		line(image.Pt(lo+arm, hi), image.Pt(lo, hi)), line(image.Pt(lo, hi), image.Pt(lo, hi-arm)),
		{Tool: annotate.ToolPen, Color: white, Width: 110, Points: []image.Point{{size / 2, size / 2}}},
	}
	write("assets/icon.png", raster.Compose(img, shapes))

	tray := image.NewRGBA(image.Rect(0, 0, 32, 32))
	copy(tray.Pix, sui.TrayIcon(32, annotate.Color{A: 255}))
	write("assets/tray-template.png", tray)
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// roundedCoverage is the anti-aliased coverage of a rounded square.
func roundedCoverage(x, y, lo, hi, r float64) float64 {
	cx := math.Min(math.Max(x, lo+r), hi-r)
	cy := math.Min(math.Max(y, lo+r), hi-r)
	d := math.Hypot(x-cx, y-cy)
	return math.Min(math.Max(r-d+0.5, 0), 1)
}

func write(path string, img *image.RGBA) {
	data, err := raster.EncodePNG(img)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
}
