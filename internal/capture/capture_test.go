package capture

import (
	"errors"
	"image"
	"testing"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
)

type fakeSource struct {
	displays []platform.Display
	fail     error
}

func (f fakeSource) Displays() ([]platform.Display, error) { return f.displays, nil }
func (f fakeSource) CaptureDisplay(id uint32) (*image.RGBA, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	for _, d := range f.displays {
		if d.ID == id {
			return image.NewRGBA(image.Rect(0, 0, int(d.Bounds.W*d.Scale), int(d.Bounds.H*d.Scale))), nil
		}
	}
	return nil, errors.New("no such display")
}
func (f fakeSource) Windows() ([]platform.Rect, error) {
	return []platform.Rect{{X: 1, Y: 2, W: 3, H: 4}}, nil
}

func TestBeginCapturesEveryDisplay(t *testing.T) {
	src := fakeSource{displays: []platform.Display{
		{ID: 1, Bounds: platform.Rect{W: 1440, H: 900}, Scale: 2, Primary: true},
		{ID: 7, Bounds: platform.Rect{X: 1440, W: 1920, H: 1080}, Scale: 1.5},
	}}
	s, err := Begin(src, ModeLongShot)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 2 || s.Mode != ModeLongShot || len(s.Windows) != 1 {
		t.Fatalf("session = %+v", s)
	}
	f := s.Frames[1]
	if f.Scale() != 1.5 || f.Bitmap.Width != 2880 || f.Bitmap.ID == s.Frames[0].Bitmap.ID {
		t.Fatalf("frame scale %v bitmap %dx%d", f.Scale(), f.Bitmap.Width, f.Bitmap.Height)
	}
	s.Release()
	if s.Frames != nil {
		t.Fatal("Release keeps frames")
	}
}

func TestBeginPermission(t *testing.T) {
	src := fakeSource{displays: []platform.Display{{ID: 1, Bounds: platform.Rect{W: 10, H: 10}, Scale: 1}}, fail: platform.ErrPermissionDenied}
	if _, err := Begin(src, ModeRegion); !errors.Is(err, platform.ErrPermissionDenied) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Begin(fakeSource{}, ModeRegion); !errors.Is(err, ErrNoDisplays) {
		t.Fatalf("err = %v", err)
	}
}

func TestCoordinates(t *testing.T) {
	d := platform.Display{ID: 1, Bounds: platform.Rect{X: -1280, Y: 100, W: 1280, H: 800}, Scale: 2}
	f := NewFrame(d, image.NewRGBA(image.Rect(0, 0, 2560, 1600)))
	if p := f.ToImage(vui.Point{X: 100.4, Y: 50}); p != (image.Point{X: 201, Y: 100}) {
		t.Fatalf("ToImage = %v", p)
	}
	if p := f.ToWindow(image.Point{X: 201, Y: 100}); p.X != 100.5 || p.Y != 50 {
		t.Fatalf("ToWindow = %v", p)
	}
	r := f.RectToImage(vui.Rect{X: 1200, Y: 700, Width: 200, Height: 200})
	if r != image.Rect(2400, 1400, 2560, 1600) {
		t.Fatalf("RectToImage clips: %v", r)
	}
	if sr := f.ScreenRect(vui.Rect{X: 10, Y: 20, Width: 30, Height: 40}); sr != (platform.Rect{X: -1270, Y: 120, W: 30, H: 40}) {
		t.Fatalf("ScreenRect = %+v", sr)
	}
	if c := f.Crop(image.Rect(0, 0, 10, 20)); c.Bounds().Dy() != 20 {
		t.Fatal("crop")
	}
	if s := f.Size(); s.Width != 1280 || s.Height != 800 {
		t.Fatal("size")
	}
}
