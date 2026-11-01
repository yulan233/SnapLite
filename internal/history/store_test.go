package history

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClip struct {
	text string
	png  []byte
}

func (c *fakeClip) SetText(t string) error     { c.text, c.png = t, nil; return nil }
func (c *fakeClip) SetImagePNG(p []byte) error { c.png, c.text = p, ""; return nil }

func picture(w, h int, seed uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = seed + uint8(i*7)
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	return img
}

func open(t *testing.T, dir string, l Limits) *Store {
	t.Helper()
	s, err := Open(dir, l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Flush)
	return s
}

func TestDeduplicateMovesToFront(t *testing.T) {
	s := open(t, t.TempDir(), Limits{})
	a, _ := s.AddText("alpha")
	s.AddText("beta")
	again, _ := s.AddText("alpha")
	items := s.Items()
	if len(items) != 2 || items[0].ID != a.ID || again.ID != a.ID {
		t.Fatalf("items = %+v", items)
	}
	img := picture(20, 10, 1)
	i1, err := s.AddImage(img)
	if err != nil {
		t.Fatal(err)
	}
	// The same pixels encoded again are the same item.
	i2, _ := s.AddImage(picture(20, 10, 1))
	if i1.ID != i2.ID || len(s.Items()) != 3 || i1.Size != [2]int{20, 10} {
		t.Fatalf("image dedupe: %+v %+v", i1, i2)
	}
	if _, err := s.AddText("   \n"); err == nil {
		t.Fatal("blank text is not recorded")
	}
}

func TestCapacityEviction(t *testing.T) {
	s := open(t, t.TempDir(), Limits{MaxItems: 3})
	var first Item
	for i := 0; i < 5; i++ {
		it, _ := s.AddText(strings.Repeat("x", i+1))
		if i == 0 {
			first = it
		}
	}
	items := s.Items()
	if len(items) != 3 || items[0].Text != "xxxxx" || items[2].Text != "xxx" {
		t.Fatalf("items = %+v", items)
	}
	if err := s.Delete(first.ID); err == nil {
		t.Fatal("the oldest item was evicted")
	}
}

func TestByteEviction(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Limits{MaxItems: 100})
	old, _ := s.AddImage(picture(30, 30, 1))
	// Room for about one and a half pictures: adding a second evicts the oldest.
	s.SetLimits(Limits{MaxItems: 100, MaxBytes: old.Bytes * 3 / 2})
	newer, _ := s.AddImage(picture(30, 30, 9))
	s.AddText("text does not count towards the image budget")
	s.Flush()
	items := s.Items()
	if len(items) != 2 || items[1].ID != newer.ID {
		t.Fatalf("items = %+v", items)
	}
	if _, err := os.Stat(filepath.Join(dir, old.ID+".png")); !os.IsNotExist(err) {
		t.Fatal("the evicted image file is removed")
	}
	if _, err := os.Stat(filepath.Join(dir, newer.ID+".png")); err != nil {
		t.Fatal("the kept image file exists")
	}
}

func TestPersistenceAndCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, Limits{})
	s.AddText("keep me")
	img, _ := s.AddImage(picture(8, 8, 3))
	gone, _ := s.AddImage(picture(8, 8, 4))
	s.Flush()
	_ = os.Remove(filepath.Join(dir, gone.ID+".png"))
	again := open(t, dir, Limits{})
	items := again.Items()
	if len(items) != 2 || items[0].ID != img.ID || items[1].Text != "keep me" {
		t.Fatalf("reloaded = %+v", items)
	}
	loaded, err := again.LoadImage(img.ID)
	if err != nil || loaded.Bounds().Dx() != 8 || ImageHash(loaded) != img.Hash {
		t.Fatalf("load image: %v", err)
	}
	again.Flush()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := open(t, dir, Limits{})
	if len(broken.Items()) != 0 {
		t.Fatal("a corrupt index starts empty")
	}
	broken.AddText("fresh")
	broken.Flush()
	if len(open(t, dir, Limits{}).Items()) != 1 {
		t.Fatal("the index is rewritten")
	}
}

func TestRestoreMovesToFront(t *testing.T) {
	s := open(t, t.TempDir(), Limits{})
	img, _ := s.AddImage(picture(4, 4, 5))
	txt, _ := s.AddText("hello")
	clip := &fakeClip{}
	if err := s.Restore(img.ID, clip); err != nil {
		t.Fatal(err)
	}
	if len(clip.png) == 0 || s.Items()[0].ID != img.ID {
		t.Fatal("image restore")
	}
	if err := s.Restore(txt.ID, clip); err != nil || clip.text != "hello" || s.Items()[0].ID != txt.ID {
		t.Fatal("text restore")
	}
	if err := s.Restore("nope", clip); err != ErrNotFound {
		t.Fatal("unknown id")
	}
}

func TestTruncateClearAndThumbnail(t *testing.T) {
	s := open(t, t.TempDir(), Limits{})
	long := strings.Repeat("长", MaxTextBytes) // 3 bytes per rune
	it, _ := s.AddText(long)
	if !it.Truncated || len(it.Text) > MaxTextBytes || !strings.HasPrefix(long, it.Text) {
		t.Fatalf("truncation: %d bytes", len(it.Text))
	}
	img, _ := s.AddImage(picture(400, 100, 2))
	b, err := s.LoadThumbnail(img.ID)
	if err != nil || b.Width != ThumbHeight || b.Height != 48 {
		t.Fatalf("thumbnail %dx%d %v", b.Width, b.Height, err)
	}
	if _, ok := s.Thumbnail(img.ID); !ok {
		t.Fatal("thumbnail cached")
	}
	v := s.Version()
	s.Clear()
	if len(s.Items()) != 0 || s.Version() == v {
		t.Fatal("clear")
	}
	s.Close(time.Second)
}
