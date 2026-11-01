package longshot

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

func TestBar(t *testing.T) {
	bar := NewBar(sui.Light(), ManualMessage, true)
	finished, canceled, auto := 0, 0, 0
	bar.OnFinish = func() { finished++ }
	bar.OnCancel = func() { canceled++ }
	bar.OnEnableAuto = func() { auto++ }
	var canvas vui.Canvas
	d, err := app.NewDriver(bar.Config(vui.Point{X: 10, Y: 20}), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	has := func(s string) bool {
		for _, l := range d.Snapshot().Texts {
			if strings.Contains(l.Value, s) {
				return true
			}
		}
		return false
	}
	click := func(id vui.ID) {
		for _, h := range d.Snapshot().Hits {
			if h.ID == id {
				if err := d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("no %s", sui.Tree.Text(id))
	}
	if !has("请滚动目标区域") || d.Snapshot().Width != BarWideWidth {
		t.Fatal("manual message and the wide bar")
	}
	bar.Update(Progress{Frames: 3, Height: 2400, Message: RunningMessage(3, 2400)})
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	if !has("已拼接 3 屏 · 2400px") {
		for _, l := range d.Snapshot().Texts {
			t.Log(l.Value)
		}
		t.Fatal("progress text")
	}
	click(barFinish)
	click(barCancel)
	click(barAuto)
	if finished != 1 || canceled != 1 || auto != 1 {
		t.Fatalf("finish=%d cancel=%d auto=%d", finished, canceled, auto)
	}
	if err := d.Key(vui.KeyEscape, 0); err != nil || canceled != 2 {
		t.Fatal("Escape cancels")
	}
}

func TestPlaceBar(t *testing.T) {
	display := platform.Rect{X: 0, Y: 0, W: 1440, H: 900}
	r := PlaceBar(platform.Rect{X: 100, Y: 100, W: 400, H: 300}, display, 300, 44)
	if r.Y != 408 || r.X != 150 {
		t.Fatalf("below and centred: %+v", r)
	}
	r = PlaceBar(platform.Rect{X: 100, Y: 300, W: 400, H: 580}, display, 300, 44)
	if r.Y != 248 {
		t.Fatalf("above: %+v", r)
	}
	sel := platform.Rect{X: 0, Y: 20, W: 1440, H: 870}
	r = PlaceBar(sel, display, 300, 44)
	if r.Y+r.H > display.H || r.X+r.W > display.W {
		t.Fatalf("on screen: %+v", r)
	}
	r = PlaceBar(platform.Rect{X: 1400, Y: 100, W: 40, H: 40}, display, 300, 44)
	if r.X+r.W > 1440-7 {
		t.Fatalf("clamped horizontally: %+v", r)
	}
}
