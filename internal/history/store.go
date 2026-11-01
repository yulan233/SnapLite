package history

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/raster"
)

// Kind is what a history item holds.
type Kind string

const (
	KindText  Kind = "text"
	KindImage Kind = "image"
)

// Item is one clipboard entry. Images live in <id>.png next to index.json.
type Item struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Hash      string    `json:"hash"`
	Created   time.Time `json:"created"`
	Size      [2]int    `json:"size,omitempty"` // image width and height
	Bytes     int64     `json:"bytes"`
	Text      string    `json:"text,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
}

// Limits bound the history.
type Limits struct {
	MaxItems int
	MaxBytes int64 // total size of the stored images
}

// Clipboard is where Restore puts an item back.
type Clipboard interface {
	SetText(text string) error
	SetImagePNG(png []byte) error
}

// MaxTextBytes is how much of a long text is kept.
const MaxTextBytes = 64 << 10

// ErrNotFound means no item has the id.
var ErrNotFound = errors.New("history: no such item")

// Store is the persisted history. Methods are safe for concurrent use; disk
// writes run in order on one worker goroutine that only exists while there
// is work, so an idle store costs nothing.
type Store struct {
	dir    string
	limits Limits

	mu       sync.Mutex
	items    []Item // newest first
	thumbs   map[string]vui.Bitmap
	version  uint64
	onChange func()
	nextID   int64

	jobsMu  sync.Mutex
	jobs    []func()
	running bool
	idle    *sync.Cond
}

// Open reads dir/index.json, dropping entries whose files are missing or
// whose records are damaged. A missing or corrupt index starts empty.
func Open(dir string, limits Limits) (*Store, error) {
	if limits.MaxItems <= 0 {
		limits.MaxItems = 100
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 500 << 20
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, limits: limits, thumbs: map[string]vui.Bitmap{}}
	s.idle = sync.NewCond(&s.jobsMu)
	data, err := os.ReadFile(s.indexPath())
	if err == nil {
		var items []Item
		if json.Unmarshal(data, &items) == nil {
			for _, it := range items {
				if it.ID == "" || it.Hash == "" || (it.Kind != KindText && it.Kind != KindImage) {
					continue
				}
				if it.Kind == KindImage {
					if _, err := os.Stat(s.imagePath(it.ID)); err != nil {
						continue
					}
				}
				s.items = append(s.items, it)
			}
		}
	}
	if len(s.items) > 0 && err == nil {
		s.mu.Lock()
		evicted := s.evictLocked()
		s.mu.Unlock()
		s.schedule(s.persistJob(evicted))
	}
	return s, nil
}

func (s *Store) indexPath() string          { return filepath.Join(s.dir, "index.json") }
func (s *Store) imagePath(id string) string { return filepath.Join(s.dir, id+".png") }
func (s *Store) Dir() string                { return s.dir }
func (s *Store) SetOnChange(fn func())      { s.mu.Lock(); s.onChange = fn; s.mu.Unlock() }
func (s *Store) Version() uint64            { s.mu.Lock(); defer s.mu.Unlock(); return s.version }
func (s *Store) Limits() Limits             { return s.limits }
func (s *Store) SetLimits(l Limits)         { s.mu.Lock(); s.limits = l; s.mu.Unlock() }
func hashBytes(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ImageHash hashes decoded pixels, so one picture in two encodings is one item.
func ImageHash(img *image.RGBA) string {
	b := img.Bounds()
	var dims [8]byte
	binary.LittleEndian.PutUint32(dims[0:], uint32(b.Dx()))
	binary.LittleEndian.PutUint32(dims[4:], uint32(b.Dy()))
	h := sha256.New()
	h.Write(dims[:])
	for y := b.Min.Y; y < b.Max.Y; y++ {
		off := img.PixOffset(b.Min.X, y)
		h.Write(img.Pix[off : off+b.Dx()*4])
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func (s *Store) newIDLocked() string {
	n := time.Now().UnixNano()
	if n <= s.nextID {
		n = s.nextID + 1
	}
	s.nextID = n
	return strconv.FormatInt(n, 36)
}

// Items returns a copy, newest first.
func (s *Store) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Item(nil), s.items...)
}

func (s *Store) changedLocked() func() {
	s.version++
	return s.onChange
}

func notify(fn func()) {
	if fn != nil {
		fn()
	}
}

// moveToFrontLocked moves an item with hash to the front; it reports the item.
func (s *Store) moveToFrontLocked(hash string) (Item, bool) {
	for i, it := range s.items {
		if it.Hash == hash {
			copy(s.items[1:i+1], s.items[:i])
			s.items[0] = it
			return it, true
		}
	}
	return Item{}, false
}

// evictLocked drops the oldest items beyond the limits and returns them.
func (s *Store) evictLocked() []Item {
	var evicted []Item
	total := int64(0)
	for _, it := range s.items {
		if it.Kind == KindImage {
			total += it.Bytes
		}
	}
	for len(s.items) > 1 && (len(s.items) > s.limits.MaxItems || total > s.limits.MaxBytes) {
		last := s.items[len(s.items)-1]
		s.items = s.items[:len(s.items)-1]
		if last.Kind == KindImage {
			total -= last.Bytes
		}
		delete(s.thumbs, last.ID)
		evicted = append(evicted, last)
	}
	return evicted
}

// AddText records text. Blank text is ignored; text over 64 KB is cut. The
// same text as an existing item moves that item to the front.
func (s *Store) AddText(text string) (Item, error) {
	if strings.TrimSpace(text) == "" {
		return Item{}, errors.New("history: blank text")
	}
	truncated := false
	if len(text) > MaxTextBytes {
		cut := MaxTextBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, truncated = text[:cut], true
	}
	hash := hashBytes([]byte("text:"), []byte(text))
	s.mu.Lock()
	if it, ok := s.moveToFrontLocked(hash); ok {
		fn := s.changedLocked()
		s.mu.Unlock()
		s.schedule(s.persistJob(nil))
		notify(fn)
		return it, nil
	}
	it := Item{ID: s.newIDLocked(), Kind: KindText, Hash: hash, Created: time.Now(), Text: text, Truncated: truncated, Bytes: int64(len(text))}
	s.items = append([]Item{it}, s.items...)
	evicted := s.evictLocked()
	fn := s.changedLocked()
	s.mu.Unlock()
	s.schedule(s.persistJob(evicted))
	notify(fn)
	return it, nil
}

// AddImage records a picture, encoding it as PNG. Call it off the event loop.
func (s *Store) AddImage(img *image.RGBA) (Item, error) { return s.AddImagePNG(img, nil) }

// AddImagePNG records a picture whose PNG encoding is already known (nil to
// encode here). It blocks on hashing and encoding; call it off the event loop.
func (s *Store) AddImagePNG(img *image.RGBA, data []byte) (Item, error) {
	hash := ImageHash(img)
	s.mu.Lock()
	if it, ok := s.moveToFrontLocked(hash); ok {
		fn := s.changedLocked()
		s.mu.Unlock()
		s.schedule(s.persistJob(nil))
		notify(fn)
		return it, nil
	}
	s.mu.Unlock()
	if data == nil {
		var err error
		if data, err = raster.EncodePNG(img); err != nil {
			return Item{}, err
		}
	}
	b := img.Bounds()
	s.mu.Lock()
	if it, ok := s.moveToFrontLocked(hash); ok { // added meanwhile
		s.mu.Unlock()
		return it, nil
	}
	it := Item{ID: s.newIDLocked(), Kind: KindImage, Hash: hash, Created: time.Now(), Size: [2]int{b.Dx(), b.Dy()}, Bytes: int64(len(data))}
	s.items = append([]Item{it}, s.items...)
	evicted := s.evictLocked()
	fn := s.changedLocked()
	s.mu.Unlock()
	path := s.imagePath(it.ID)
	persist := s.persistJob(evicted)
	s.schedule(func() {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return
		}
		persist()
	})
	notify(fn)
	return it, nil
}

// Delete removes one item.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	for i, it := range s.items {
		if it.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			delete(s.thumbs, id)
			fn := s.changedLocked()
			s.mu.Unlock()
			s.schedule(s.persistJob([]Item{it}))
			notify(fn)
			return nil
		}
	}
	s.mu.Unlock()
	return ErrNotFound
}

// Clear removes every item.
func (s *Store) Clear() error {
	s.mu.Lock()
	removed := s.items
	s.items = nil
	s.thumbs = map[string]vui.Bitmap{}
	fn := s.changedLocked()
	s.mu.Unlock()
	s.schedule(s.persistJob(removed))
	notify(fn)
	return nil
}

func (s *Store) find(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// ImagePNG reads an image item's PNG. Pending writes are flushed first.
func (s *Store) ImagePNG(id string) ([]byte, error) {
	it, ok := s.find(id)
	if !ok || it.Kind != KindImage {
		return nil, ErrNotFound
	}
	s.Flush()
	return os.ReadFile(s.imagePath(id))
}

// LoadImage decodes an image item.
func (s *Store) LoadImage(id string) (*image.RGBA, error) {
	data, err := s.ImagePNG(id)
	if err != nil {
		return nil, err
	}
	return raster.DecodePNG(data)
}

// Restore writes the item back to the clipboard and moves it to the front.
func (s *Store) Restore(id string, clip Clipboard) error {
	it, ok := s.find(id)
	if !ok {
		return ErrNotFound
	}
	var err error
	switch it.Kind {
	case KindText:
		err = clip.SetText(it.Text)
	case KindImage:
		var data []byte
		if data, err = s.ImagePNG(id); err == nil {
			err = clip.SetImagePNG(data)
		}
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.moveToFrontLocked(it.Hash)
	fn := s.changedLocked()
	s.mu.Unlock()
	s.schedule(s.persistJob(nil))
	notify(fn)
	return nil
}

// ThumbHeight is the largest thumbnail side in image pixels: 96 logical
// pixels at 2× (docs/01 §2.5).
const ThumbHeight = 192

// Thumbnail returns the cached thumbnail of an image item.
func (s *Store) Thumbnail(id string) (vui.Bitmap, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.thumbs[id]
	return b, ok
}

// LoadThumbnail decodes an item and caches its thumbnail. It blocks; run it
// on a goroutine and repaint afterwards.
func (s *Store) LoadThumbnail(id string) (vui.Bitmap, error) {
	if b, ok := s.Thumbnail(id); ok {
		return b, nil
	}
	img, err := s.LoadImage(id)
	if err != nil {
		return vui.Bitmap{}, err
	}
	b := raster.Bitmap(raster.Thumbnail(img, ThumbHeight))
	s.mu.Lock()
	if _, still := s.thumbs[id]; !still {
		if _, ok := s.findLocked(id); ok {
			s.thumbs[id] = b
		}
	}
	s.mu.Unlock()
	return b, nil
}

func (s *Store) findLocked(id string) (Item, bool) {
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// persistJob writes the index as it is when the job runs and removes the
// files of removed items.
func (s *Store) persistJob(removed []Item) func() {
	return func() {
		for _, it := range removed {
			if it.Kind == KindImage {
				_ = os.Remove(s.imagePath(it.ID))
			}
		}
		s.mu.Lock()
		data, err := json.MarshalIndent(s.items, "", " ")
		s.mu.Unlock()
		if err != nil {
			return
		}
		tmp := s.indexPath() + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, s.indexPath())
		}
	}
}

// schedule queues a disk job. The worker goroutine is started on demand and
// exits when the queue is empty: no goroutine waits while nothing happens.
func (s *Store) schedule(job func()) {
	s.jobsMu.Lock()
	s.jobs = append(s.jobs, job)
	if !s.running {
		s.running = true
		go s.work()
	}
	s.jobsMu.Unlock()
}

func (s *Store) work() {
	for {
		s.jobsMu.Lock()
		if len(s.jobs) == 0 {
			s.running = false
			s.idle.Broadcast()
			s.jobsMu.Unlock()
			return
		}
		job := s.jobs[0]
		s.jobs = s.jobs[1:]
		s.jobsMu.Unlock()
		job()
	}
}

// Flush waits for queued disk writes.
func (s *Store) Flush() {
	s.jobsMu.Lock()
	for s.running {
		s.idle.Wait()
	}
	s.jobsMu.Unlock()
}

// Close waits up to timeout for pending writes.
func (s *Store) Close(timeout time.Duration) {
	done := make(chan struct{})
	go func() { s.Flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
