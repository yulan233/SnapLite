package longshot

import (
	"bytes"
	"image"
)

// AddKind is what Stitcher.Add did with a frame.
type AddKind uint8

const (
	// First is the first frame: it starts the result.
	First AddKind = iota
	// Appended means the frame scrolled and its new rows were added.
	Appended
	// NoChange means the frame shows the same content as the previous one.
	NoChange
	// NoOverlap means no trustworthy overlap was found; the frame was dropped.
	NoOverlap
)

func (k AddKind) String() string {
	return [...]string{"First", "Appended", "NoChange", "NoOverlap"}[k]
}

// AddResult reports one Add.
type AddResult struct {
	Kind AddKind
	// Rows is how many rows the result grew by (Appended), the scroll offset.
	Rows int
}

// Stitcher joins frames of a region that only scrolls vertically into one
// long image (docs/05 §5.2). Fixed headers and footers are detected as rows
// that did not move: a header appears once at the top, a footer once at the
// bottom.
type Stitcher struct {
	w, h    int
	prev    []uint64 // row signatures of the previous accepted frame
	solid   []bool
	scratch []uint64
	lastDy  int

	chunks [][]byte // the result's rows, Stride w*4, in order
	rows   []int    // rows in each chunk
	total  int
}

// minMatch is the match rate below which an offset is not trusted.
const (
	minMatch   = 0.90
	goodMatch  = 0.98
	minOverlap = 16
)

// Width and Height are the result's size so far.
func (s *Stitcher) Width() int  { return s.w }
func (s *Stitcher) Height() int { return s.total }

// rowSignatures hashes each row with FNV-1a over its colour channels shifted
// right by two bits (tolerating faint noise), ignoring alpha. solid reports
// rows of one colour, whose signatures repeat and would mislead matching.
func rowSignatures(img *image.RGBA, sig []uint64, solid []bool) {
	b := img.Bounds()
	w := b.Dx()
	for y := 0; y < b.Dy(); y++ {
		row := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):]
		row = row[:w*4]
		h := uint64(14695981039346656037)
		one := true
		r0, g0, b0 := row[0]>>2, row[1]>>2, row[2]>>2
		for x := 0; x < len(row); x += 4 {
			r, g, bl := row[x]>>2, row[x+1]>>2, row[x+2]>>2
			if r != r0 || g != g0 || bl != b0 {
				one = false
			}
			h = (h ^ uint64(r)) * 1099511628211
			h = (h ^ uint64(g)) * 1099511628211
			h = (h ^ uint64(bl)) * 1099511628211
		}
		sig[y] = h
		solid[y] = one
	}
}

func rowsOf(img *image.RGBA, from, to int) []byte {
	b := img.Bounds()
	w := b.Dx()
	out := make([]byte, 0, (to-from)*w*4)
	for y := from; y < to; y++ {
		off := img.PixOffset(b.Min.X, b.Min.Y+y)
		out = append(out, img.Pix[off:off+w*4]...)
	}
	return out
}

func (s *Stitcher) appendRows(data []byte, n int) {
	if n <= 0 {
		return
	}
	s.chunks = append(s.chunks, data)
	s.rows = append(s.rows, n)
	s.total += n
}

// truncate drops the last n rows of the result.
func (s *Stitcher) truncate(n int) {
	for n > 0 && len(s.rows) > 0 {
		last := len(s.rows) - 1
		if s.rows[last] <= n {
			n -= s.rows[last]
			s.total -= s.rows[last]
			s.chunks, s.rows = s.chunks[:last], s.rows[:last]
			continue
		}
		s.rows[last] -= n
		s.chunks[last] = s.chunks[last][:s.rows[last]*s.w*4]
		s.total -= n
		n = 0
	}
}

// Add offers the next frame. Every frame must have the first frame's size.
func (s *Stitcher) Add(img *image.RGBA) AddResult {
	b := img.Bounds()
	if s.prev == nil {
		s.w, s.h = b.Dx(), b.Dy()
		s.prev = make([]uint64, s.h)
		s.solid = make([]bool, s.h)
		s.scratch = make([]uint64, s.h)
		rowSignatures(img, s.prev, s.solid)
		s.appendRows(rowsOf(img, 0, s.h), s.h)
		return AddResult{Kind: First}
	}
	if b.Dx() != s.w || b.Dy() != s.h {
		return AddResult{Kind: NoOverlap}
	}
	cur, solid := s.scratch, make([]bool, s.h)
	rowSignatures(img, cur, solid)
	h := s.h
	top := 0
	for top < h && cur[top] == s.prev[top] {
		top++
	}
	if top == h {
		return AddResult{Kind: NoChange}
	}
	bottom := 0
	for bottom < h-top && cur[h-1-bottom] == s.prev[h-1-bottom] {
		bottom++
	}
	if top+bottom >= h-8 {
		return AddResult{Kind: NoChange}
	}
	dy, ok := s.offset(cur, solid, top, bottom)
	if !ok {
		return AddResult{Kind: NoOverlap}
	}
	// Result minus the old footer rows, then the newly revealed rows of the
	// scrolling region, then the new footer.
	s.truncate(bottom)
	s.appendRows(rowsOf(img, h-bottom-dy, h-bottom), dy)
	s.appendRows(rowsOf(img, h-bottom, h), bottom)
	s.lastDy = dy
	s.prev, s.scratch = cur, s.prev
	s.solid = solid
	return AddResult{Kind: Appended, Rows: dy}
}

// offset finds how far the content moved up: new row y equals previous row
// y+dy inside the scrolling region [top, h-bottom). Candidates near the last
// offset are tried first.
func (s *Stitcher) offset(cur []uint64, solid []bool, top, bottom int) (int, bool) {
	h := s.h
	end := h - bottom
	scrollH := end - top
	maxDy := scrollH - minOverlap
	if maxDy < 1 {
		return 0, false
	}
	textured := 0
	for y := top; y < end; y++ {
		if !solid[y] {
			textured++
		}
	}
	useAll := textured < minOverlap
	best, bestRate := 0, 0.0
	try := func(dy int) bool {
		matches, considered := 0, 0
		for y := top; y+dy < end; y++ {
			if !useAll && solid[y] {
				continue
			}
			considered++
			if cur[y] == s.prev[y+dy] {
				matches++
			}
		}
		if considered < minOverlap/2 {
			return false
		}
		rate := float64(matches) / float64(considered)
		if rate > bestRate {
			best, bestRate = dy, rate
		}
		return rate >= goodMatch
	}
	start := min(max(s.lastDy, 1), maxDy)
	if try(start) {
		return start, true
	}
	for d := 1; ; d++ {
		lo, hi := start-d, start+d
		if lo < 1 && hi > maxDy {
			break
		}
		if lo >= 1 && try(lo) {
			return lo, true
		}
		if hi <= maxDy && try(hi) {
			return hi, true
		}
	}
	if bestRate < minMatch {
		return 0, false
	}
	return best, true
}

// Result copies the stitched rows into one image.
func (s *Stitcher) Result() *image.RGBA {
	if s.total == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, s.w, s.total))
	off := 0
	for _, c := range s.chunks {
		off += copy(img.Pix[off:], c)
	}
	return img
}

// sameImage reports whether two captures are pixel-identical.
func sameImage(a, b *image.RGBA) bool {
	return a != nil && b != nil && a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}
