package capture

import (
	"errors"
	"image"
	"math"
	"sync"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
	"snaplite/internal/raster"
)

// Frame is one display's frozen picture.
type Frame struct {
	Display platform.Display
	Image   *image.RGBA // physical pixels
	Bitmap  vui.Bitmap  // the same pixels, premultiplied BGRA, for Viva
}

// NewFrame wraps a capture of a display.
func NewFrame(d platform.Display, img *image.RGBA) Frame {
	return Frame{Display: d, Image: img, Bitmap: raster.Bitmap(img)}
}

// Scale is image pixels per logical pixel: the capture's width over the
// display's logical width (never assumed to be 2).
func (f *Frame) Scale() float64 {
	if f.Display.Bounds.W <= 0 || f.Image == nil {
		return 1
	}
	return float64(f.Image.Bounds().Dx()) / f.Display.Bounds.W
}

// Size is the display's size in logical pixels: the overlay window's size.
func (f *Frame) Size() vui.Size {
	return vui.Size{Width: float32(f.Display.Bounds.W), Height: float32(f.Display.Bounds.H)}
}

// ToImage maps an overlay-window point to an image pixel.
func (f *Frame) ToImage(p vui.Point) image.Point {
	s := f.Scale()
	return image.Point{X: int(math.Round(float64(p.X) * s)), Y: int(math.Round(float64(p.Y) * s))}
}

// ToWindow maps an image pixel to the overlay window.
func (f *Frame) ToWindow(p image.Point) vui.Point {
	s := f.Scale()
	return vui.Point{X: float32(float64(p.X) / s), Y: float32(float64(p.Y) / s)}
}

// RectToImage maps a window rectangle to image pixels, clipped to the image.
func (f *Frame) RectToImage(r vui.Rect) image.Rectangle {
	min := f.ToImage(vui.Point{X: r.X, Y: r.Y})
	max := f.ToImage(vui.Point{X: r.X + r.Width, Y: r.Y + r.Height})
	return image.Rectangle{Min: min, Max: max}.Canon().Intersect(f.Image.Bounds())
}

// ScreenRect maps a window rectangle to screen logical coordinates.
func (f *Frame) ScreenRect(r vui.Rect) platform.Rect {
	b := f.Display.Bounds
	return platform.Rect{X: b.X + float64(r.X), Y: b.Y + float64(r.Y), W: float64(r.Width), H: float64(r.Height)}
}

// Crop copies an image rectangle.
func (f *Frame) Crop(r image.Rectangle) *image.RGBA { return raster.Crop(f.Image, r) }

// Session is one screenshot: a frame per display.
type Session struct {
	Frames []Frame
	// Windows are other applications' windows front to back in screen
	// logical coordinates, cached at the start for window snapping.
	Windows []platform.Rect
}

// Source is where a session gets its pictures; tests substitute their own.
type Source interface {
	Displays() ([]platform.Display, error)
	CaptureDisplay(id uint32) (*image.RGBA, error)
	Windows() ([]platform.Rect, error)
}

// Native is the platform's Source.
type Native struct{}

func (Native) Displays() ([]platform.Display, error)         { return platform.Displays() }
func (Native) CaptureDisplay(id uint32) (*image.RGBA, error) { return platform.CaptureDisplay(id) }
func (Native) Windows() ([]platform.Rect, error)             { return platform.ListWindows(true) }

// ErrNoDisplays means the system reported no display.
var ErrNoDisplays = errors.New("capture: no displays")

// Begin captures every display concurrently. It blocks; call it off the
// event-loop thread. A missing permission is reported as
// platform.ErrPermissionDenied.
func Begin(src Source) (*Session, error) {
	displays, err := src.Displays()
	if err != nil {
		return nil, err
	}
	if len(displays) == 0 {
		return nil, ErrNoDisplays
	}
	frames := make([]Frame, len(displays))
	errs := make([]error, len(displays))
	var wg sync.WaitGroup
	for i, d := range displays {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := src.CaptureDisplay(d.ID)
			if err != nil {
				errs[i] = err
				return
			}
			frames[i] = NewFrame(d, img)
		}()
	}
	windows, _ := src.Windows()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return &Session{Frames: frames, Windows: windows}, nil
}

// Release drops the frames so the large pictures can be collected.
func (s *Session) Release() {
	s.Frames = nil
	s.Windows = nil
}
