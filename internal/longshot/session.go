package longshot

import (
	"fmt"
	"image"
	"sync"
	"time"
)

// Mode is how the region gets scrolled.
type Mode uint8

const (
	Auto   Mode = iota // SnapLite sends wheel events
	Manual             // the user scrolls; SnapLite samples every 150 ms
)

// State of a session.
type State uint8

const (
	Running State = iota
	Paused
	Done
	Failed
	Canceled
)

// Progress is reported after every frame and at the end.
type Progress struct {
	Frames  int
	Height  int // physical pixels
	State   State
	Mode    Mode
	Message string
}

// Capturer captures the region. Scroller scrolls it by logical pixels
// (positive down). Clock waits, returning false when done closes first.
type (
	Capturer interface {
		Capture() (*image.RGBA, error)
	}
	Scroller interface {
		Scroll(dy float64) error
	}
	Clock interface {
		Wait(d time.Duration, done <-chan struct{}) bool
	}
)

// RealClock waits on a timer.
type RealClock struct{}

func (RealClock) Wait(d time.Duration, done <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-done:
		return false
	}
}

// Options configure a session.
type Options struct {
	Mode Mode
	// Height is the region's height in logical pixels; Scale converts it to
	// the captures' physical pixels.
	Height    float64
	Scale     float64
	MaxHeight int // physical pixels; 0 means 30000
	// Delay is waited before the first capture, so windows that were just
	// closed (the overlay) are gone from the screen.
	Delay    time.Duration
	Capturer Capturer
	Scroller Scroller
	Clock    Clock
	// Post delivers progress to the UI thread (platform.PostMain).
	Post func(func())
}

// Timing of the loop (docs/05 §5.1).
const (
	settleDelay   = 220 * time.Millisecond
	stableGap     = 60 * time.Millisecond
	stableLimit   = 600 * time.Millisecond
	manualPeriod  = 150 * time.Millisecond
	stepFraction  = 0.7
	stepMaxFrac   = 0.8
	smallFraction = 0.3
)

// Session drives one long shot on its own goroutine, which exits when the
// session ends: nothing is left running.
type Session struct {
	opt        Options
	onProgress func(Progress)
	done       chan struct{}
	stopOnce   sync.Once
	finished   chan struct{}

	mu       sync.Mutex
	stitcher Stitcher
	state    State
	canceled bool
}

// Start begins capturing. onProgress runs through opt.Post.
func Start(opt Options, onProgress func(Progress)) *Session {
	if opt.MaxHeight <= 0 {
		opt.MaxHeight = 30000
	}
	if opt.Scale <= 0 {
		opt.Scale = 1
	}
	if opt.Clock == nil {
		opt.Clock = RealClock{}
	}
	if opt.Post == nil {
		opt.Post = func(fn func()) { fn() }
	}
	s := &Session{opt: opt, onProgress: onProgress, done: make(chan struct{}), finished: make(chan struct{})}
	go s.run()
	return s
}

// Finish ends the session keeping what was stitched (the user's 完成).
func (s *Session) Finish() { s.stop(false) }

// Cancel ends the session and discards the result.
func (s *Session) Cancel() { s.stop(true) }

func (s *Session) stop(cancel bool) {
	s.mu.Lock()
	if cancel {
		s.canceled = true
	}
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.done) })
}

// Wait blocks until the session's goroutine has returned.
func (s *Session) Wait() { <-s.finished }

// Result is the stitched image, nil when canceled or nothing was captured.
func (s *Session) Result() *image.RGBA {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.canceled {
		return nil
	}
	return s.stitcher.Result()
}

func (s *Session) stopped() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) report(p Progress) {
	if s.onProgress == nil {
		return
	}
	fn := s.onProgress
	s.opt.Post(func() { fn(p) })
}

func (s *Session) add(img *image.RGBA) AddResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stitcher.Add(img)
}

func (s *Session) height() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stitcher.Height()
}

// RunningMessage is the bar's status text.
func RunningMessage(frames, height int) string {
	return fmt.Sprintf("正在滚动… 已拼接 %d 屏 · %dpx", frames, height)
}

// Messages for the other states.
const (
	ManualMessage    = "请滚动目标区域，完成后点击完成"
	TooFastMessage   = "滚动过快，已暂停"
	NoScrollMessage  = "目标窗口无法自动滚动，请手动滚动"
	CaptureFailedMsg = "截图失败"
)

func (s *Session) run() {
	defer close(s.finished)
	opt := s.opt
	mode := opt.Mode
	frames := 0
	end := func(state State, msg string) {
		s.mu.Lock()
		if s.canceled {
			state = Canceled
		}
		s.state = state
		s.mu.Unlock()
		s.report(Progress{Frames: frames, Height: s.height(), State: state, Mode: mode, Message: msg})
	}
	if opt.Delay > 0 && !opt.Clock.Wait(opt.Delay, s.done) {
		end(Done, "")
		return
	}
	first, err := opt.Capturer.Capture()
	if err != nil {
		end(Failed, CaptureFailedMsg)
		return
	}
	s.add(first)
	frames = 1
	running := func() {
		msg := RunningMessage(frames, s.height())
		if mode == Manual {
			msg = ManualMessage + " · " + fmt.Sprintf("已拼接 %d 屏 · %dpx", frames, s.height())
		}
		s.report(Progress{Frames: frames, Height: s.height(), State: Running, Mode: mode, Message: msg})
	}
	running()
	step := opt.Height * stepFraction
	unchanged := 0
	retried, skipScroll := false, false
	for !s.stopped() {
		var frame *image.RGBA
		if mode == Auto {
			if !skipScroll {
				if err := opt.Scroller.Scroll(step); err != nil {
					mode = Manual
					s.report(Progress{Frames: frames, Height: s.height(), State: Running, Mode: mode, Message: NoScrollMessage})
					continue
				}
			}
			skipScroll = false
			if !opt.Clock.Wait(settleDelay, s.done) {
				break
			}
			frame, err = s.stableCapture()
		} else {
			if !opt.Clock.Wait(manualPeriod, s.done) {
				break
			}
			frame, err = opt.Capturer.Capture()
		}
		if s.stopped() {
			break
		}
		if err != nil {
			end(Failed, CaptureFailedMsg)
			return
		}
		res := s.add(frame)
		switch res.Kind {
		case Appended:
			frames++
			unchanged, retried = 0, false
			if mode == Auto && float64(res.Rows)/opt.Scale < smallFraction*step {
				step = min(step*1.5, opt.Height*stepMaxFrac)
			}
			running()
		case NoChange:
			if mode == Auto {
				unchanged++
				if unchanged >= 2 {
					if frames == 1 {
						// The first scroll did nothing: the target ignores
						// synthetic wheel events (an elevated window on Windows).
						mode = Manual
						unchanged = 0
						s.report(Progress{Frames: frames, Height: s.height(), State: Running, Mode: mode, Message: NoScrollMessage})
						continue
					}
					end(Done, "")
					return
				}
			}
		case NoOverlap:
			if mode == Manual {
				s.report(Progress{Frames: frames, Height: s.height(), State: Paused, Mode: mode, Message: TooFastMessage})
				continue
			}
			if retried {
				end(Done, TooFastMessage)
				return
			}
			retried = true
			step /= 2
			if err := opt.Scroller.Scroll(-step); err != nil {
				end(Done, TooFastMessage)
				return
			}
			skipScroll = true
			s.report(Progress{Frames: frames, Height: s.height(), State: Paused, Mode: mode, Message: TooFastMessage})
		}
		if s.height() >= opt.MaxHeight {
			end(Done, "")
			return
		}
	}
	end(Done, "")
}

// stableCapture captures until two captures 60 ms apart are identical, or
// 600 ms have passed.
func (s *Session) stableCapture() (*image.RGBA, error) {
	a, err := s.opt.Capturer.Capture()
	if err != nil {
		return nil, err
	}
	for waited := time.Duration(0); waited < stableLimit; waited += stableGap {
		if !s.opt.Clock.Wait(stableGap, s.done) {
			return a, nil
		}
		b, err := s.opt.Capturer.Capture()
		if err != nil {
			return nil, err
		}
		if sameImage(a, b) {
			return b, nil
		}
		a = b
	}
	return a, nil
}
