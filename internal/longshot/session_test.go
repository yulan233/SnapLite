package longshot

import (
	"bytes"
	"image"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePage is a scrollable region over a long page.
type fakePage struct {
	mu       sync.Mutex
	page     *image.RGBA
	pos      int
	perPixel float64 // content rows moved per logical pixel of scroll
	ignore   bool    // the target ignores synthetic scrolling
	scrolls  []float64
	onCap    func(f *fakePage) // runs before each capture (a user scrolling)
}

func (f *fakePage) maxPos() int { return contentH - viewport }

func (f *fakePage) Capture() (*image.RGBA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onCap != nil {
		f.onCap(f)
	}
	return view(f.page, f.pos), nil
}

func (f *fakePage) Scroll(dy float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scrolls = append(f.scrolls, dy)
	if !f.ignore {
		f.pos = min(max(f.pos+int(dy*f.perPixel), 0), f.maxPos())
	}
	return nil
}

// instantClock never sleeps.
type instantClock struct{}

func (instantClock) Wait(_ time.Duration, done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	default:
		return true
	}
}

type recorder struct {
	mu   sync.Mutex
	last Progress
	all  []Progress
}

func (r *recorder) add(p Progress) {
	r.mu.Lock()
	r.last = p
	r.all = append(r.all, p)
	r.mu.Unlock()
}

func (r *recorder) saw(msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.all {
		if strings.Contains(p.Message, msg) {
			return true
		}
	}
	return false
}

func run(t *testing.T, page *fakePage, opt Options) (*Session, *recorder) {
	t.Helper()
	rec := &recorder{}
	opt.Capturer, opt.Scroller = page, page
	if opt.Clock == nil {
		opt.Clock = instantClock{}
	}
	if opt.Height == 0 {
		opt.Height = viewH
	}
	s := Start(opt, rec.add)
	return s, rec
}

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
	}
}

func checkGoroutines(t *testing.T, before int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goroutines: %d before, %d after", before, runtime.NumGoroutine())
}

func TestAutoScrollsToTheBottom(t *testing.T) {
	before := runtime.NumGoroutine()
	page := &fakePage{page: makePage(false), perPixel: 0.5}
	s, rec := run(t, page, Options{Mode: Auto})
	waitDone(t, s)
	if rec.last.State != Done {
		t.Fatalf("state = %v %q", rec.last.State, rec.last.Message)
	}
	got := s.Result()
	if got == nil || !bytes.Equal(got.Pix, page.page.Pix) {
		t.Fatalf("the result is the whole page: %v", got.Bounds())
	}
	if !rec.saw("已拼接") {
		t.Fatal("progress reports frames and height")
	}
	checkGoroutines(t, before)
}

func TestAutoGrowsASmallStep(t *testing.T) {
	page := &fakePage{page: makePage(false), perPixel: 0.2}
	s, _ := run(t, page, Options{Mode: Auto})
	waitDone(t, s)
	if page.scrolls[0] != 350 || page.scrolls[len(page.scrolls)-1] <= 350 || page.scrolls[len(page.scrolls)-1] > 400 {
		t.Fatalf("the step grows up to 80%% of the region: %v", page.scrolls)
	}
	if !bytes.Equal(s.Result().Pix, page.page.Pix) {
		t.Fatal("result")
	}
}

func TestAutoBacksOffWhenTooFast(t *testing.T) {
	page := &fakePage{page: makePage(false), perPixel: 1.5}
	s, rec := run(t, page, Options{Mode: Auto})
	waitDone(t, s)
	negative := false
	for _, d := range page.scrolls {
		if d < 0 {
			negative = true
		}
	}
	if !negative || !rec.saw(TooFastMessage) {
		t.Fatalf("a lost overlap halves the step and scrolls back: %v", page.scrolls)
	}
	if !bytes.Equal(s.Result().Pix, page.page.Pix) {
		t.Fatal("the retried frames still reproduce the page")
	}
}

func TestAutoGivesUpAfterTwoFailures(t *testing.T) {
	page := &fakePage{page: makePage(false), perPixel: 5}
	s, rec := run(t, page, Options{Mode: Auto})
	waitDone(t, s)
	if rec.last.State != Done || s.Result().Bounds().Dy() != viewH {
		t.Fatalf("keeps the first frame: %v %v", rec.last, s.Result().Bounds())
	}
}

func TestMaxHeight(t *testing.T) {
	page := &fakePage{page: makePage(false), perPixel: 0.5}
	s, rec := run(t, page, Options{Mode: Auto, MaxHeight: 1000})
	waitDone(t, s)
	if h := s.Result().Bounds().Dy(); rec.last.State != Done || h < 1000 || h >= pageH {
		t.Fatalf("stops at the limit: %d", h)
	}
}

func TestManualMode(t *testing.T) {
	before := runtime.NumGoroutine()
	captures := 0
	page := &fakePage{page: makePage(false)}
	page.onCap = func(f *fakePage) {
		captures++
		switch {
		case captures == 4:
			f.pos += viewport + 30 // a flick: too far for one frame
		case captures == 5:
			f.pos -= viewport // back within reach
		case captures > 1:
			f.pos += 120
		}
		f.pos = min(max(f.pos, 0), f.maxPos())
	}
	rec := &recorder{}
	reached := make(chan struct{}, 1)
	opt := Options{Mode: Manual, Height: viewH, Capturer: page, Scroller: page, Clock: instantClock{}}
	s := Start(opt, func(p Progress) {
		rec.add(p)
		if p.Height >= pageH {
			select {
			case reached <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the page bottom was never reached")
	}
	s.Finish()
	waitDone(t, s)
	if !rec.saw(ManualMessage) || !rec.saw(TooFastMessage) {
		t.Fatal("manual mode messages")
	}
	if len(page.scrolls) != 0 {
		t.Fatal("manual mode never scrolls")
	}
	if !bytes.Equal(s.Result().Pix, page.page.Pix) {
		t.Fatalf("manual result %v", s.Result().Bounds())
	}
	checkGoroutines(t, before)
}

func TestUnscrollableTargetFallsBackToManual(t *testing.T) {
	page := &fakePage{page: makePage(false), perPixel: 1, ignore: true}
	s, rec := run(t, page, Options{Mode: Auto})
	deadline := time.Now().Add(5 * time.Second)
	for !rec.saw(NoScrollMessage) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s.Finish()
	waitDone(t, s)
	if !rec.saw(NoScrollMessage) {
		t.Fatal("switches to manual when scrolling has no effect")
	}
}

func TestCancelStopsTheGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	page := &fakePage{page: makePage(false), perPixel: 0.5}
	rec := &recorder{}
	s := Start(Options{Mode: Auto, Height: viewH, Capturer: page, Scroller: page, Clock: RealClock{}}, rec.add)
	time.Sleep(20 * time.Millisecond)
	s.Cancel()
	waitDone(t, s)
	if s.Result() != nil || rec.last.State != Canceled {
		t.Fatalf("a canceled session has no result: %v", rec.last)
	}
	checkGoroutines(t, before)
}
