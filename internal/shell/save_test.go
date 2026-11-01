package shell

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"snaplite/internal/annotate"
	"snaplite/internal/config"
	"snaplite/internal/history"
	"snaplite/internal/overlay"
)

func testPicture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(i/4), 100, 180, 255
	}
	return img
}

func isPNG(b []byte) bool  { return bytes.HasPrefix(b, []byte("\x89PNG")) }
func isJPEG(b []byte) bool { return bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) }

func TestSaveOptions(t *testing.T) {
	when := time.Date(2026, 11, 1, 9, 30, 15, 0, time.Local)
	jpg := imageSaveOptions("jpg", when)
	if jpg.SuggestedName != "简截_20261101_093015.jpg" || jpg.DefaultExtension != "jpg" || jpg.Filters[0].Pattern != "*.jpg" {
		t.Fatalf("jpg options = %+v", jpg)
	}
	png := imageSaveOptions(fileExt(0), when)
	if png.SuggestedName != "简截_20261101_093015.png" || png.DefaultExtension != "png" || png.Filters[0].Pattern != "*.png" {
		t.Fatalf("png options = %+v", png)
	}
	txt := textSaveOptions(when)
	if txt.SuggestedName != "剪切板_20261101_093015.txt" || txt.DefaultExtension != "txt" || txt.Filters[0].Pattern != "*.txt" {
		t.Fatalf("txt options = %+v", txt)
	}
	if fileExt(92) != "jpg" || fileExt(0) != "png" {
		t.Fatal("fileExt")
	}
}

// render: 1× shrinks the picture by the display's scale; only a save makes a
// file in the chosen quality, the clipboard always gets PNG, a pin gets no
// encoding at all.
func TestRender(t *testing.T) {
	crop := testPicture(200, 100)
	shapes := []annotate.Shape{{Tool: annotate.ToolRect, Color: annotate.Palette[0], Width: 4, Points: []image.Point{{110, 60}, {190, 120}}}}
	res := overlay.Result{Kind: overlay.Save, Sel: image.Rect(100, 50, 300, 150), Shapes: shapes}

	out, err := render(crop, res, 2, true, 80)
	if err != nil {
		t.Fatal(err)
	}
	if out.img.Bounds() != image.Rect(0, 0, 100, 50) || out.scale != 1 {
		t.Fatalf("1× result %v at %v", out.img.Bounds(), out.scale)
	}
	if !isPNG(out.png) || !isJPEG(out.file) || out.ext != "jpg" {
		t.Fatalf("save: png %v, file jpeg %v, ext %q", isPNG(out.png), isJPEG(out.file), out.ext)
	}
	if back, err := jpeg.Decode(bytes.NewReader(out.file)); err != nil || back.Bounds().Dx() != 100 {
		t.Fatalf("the file is the 1× picture: %v", err)
	}

	res.Kind = overlay.Done
	out, err = render(crop, res, 2, false, 80)
	if err != nil || out.img.Bounds() != image.Rect(0, 0, 200, 100) || out.scale != 2 || !isPNG(out.png) || out.file != nil {
		t.Fatalf("done at the original size: %v %v %v", err, out.img.Bounds(), out.scale)
	}
	// The shape is in capture pixels; the crop starts at Sel.Min, so its left
	// edge is at x = 10 of the result.
	if c := out.img.RGBAAt(50, 40); c != crop.RGBAAt(50, 40) {
		t.Fatalf("inside the outline the picture is unchanged: %v", c)
	}
	if c := out.img.RGBAAt(10, 40); c == crop.RGBAAt(10, 40) {
		t.Fatal("the rectangle is composed onto the picture")
	}

	res.Kind = overlay.Save
	if out, _ = render(crop, res, 2, false, 0); out.ext != "png" || !isPNG(out.file) {
		t.Fatalf("PNG quality saves PNG: %q", out.ext)
	}
	res.Kind = overlay.Pin
	out, err = render(crop, res, 2, true, 80)
	if err != nil || out.png != nil || out.file != nil || out.img.Bounds().Dx() != 100 {
		t.Fatalf("a pin is only the picture: %v %+v", err, out.img.Bounds())
	}
}

func testShell(t *testing.T, dialog func(app.SaveFileOptions) (string, error)) (*Shell, *bytes.Buffer) {
	t.Helper()
	cfg, err := config.LoadFrom(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	return &Shell{cfg: cfg, log: log.New(io.MultiWriter(&logged), "", 0), saveDialog: dialog}, &logged
}

func TestAskSavePath(t *testing.T) {
	s, logged := testShell(t, func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled })
	if _, ok := s.askSavePath(textSaveOptions(time.Now())); ok || logged.Len() != 0 || s.cfg.LastSaveDir != "" {
		t.Fatalf("a cancel is quiet: ok %v, log %q", ok, logged.String())
	}
	s.saveDialog = func(app.SaveFileOptions) (string, error) { return "", errors.New("boom") }
	if _, ok := s.askSavePath(textSaveOptions(time.Now())); ok || logged.Len() == 0 {
		t.Fatal("a failure is logged")
	}
	dir := t.TempDir()
	s.saveDialog = func(app.SaveFileOptions) (string, error) { return filepath.Join(dir, "a.txt"), nil }
	if path, ok := s.askSavePath(textSaveOptions(time.Now())); !ok || path != filepath.Join(dir, "a.txt") || s.cfg.LastSaveDir != dir {
		t.Fatalf("path %q ok %v last %q", path, ok, s.cfg.LastSaveDir)
	}
}

func TestSaveTextWritesUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	var asked app.SaveFileOptions
	s, _ := testShell(t, func(o app.SaveFileOptions) (string, error) { asked = o; return path, nil })
	s.saveText("你好，简截\nline 2")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "你好，简截\nline 2" || asked.DefaultExtension != "txt" {
		t.Fatalf("saved %q (%v), asked %+v", data, err, asked)
	}
}

func TestHistoryItemSaving(t *testing.T) {
	store, err := history.Open(t.TempDir(), history.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Flush()
	text, _ := store.AddText("复制的文字")
	pic, err := store.AddImage(testPicture(64, 32))
	if err != nil {
		t.Fatal(err)
	}

	// The dialog offers what the item and the quality call for.
	var asked []app.SaveFileOptions
	s, _ := testShell(t, func(o app.SaveFileOptions) (string, error) { asked = append(asked, o); return "", app.ErrCanceled })
	s.cfg.Capture.Quality = config.QualityMedium
	s.saveHistoryItem(store, pic)
	s.saveHistoryItem(store, text)
	s.cfg.Capture.Quality = config.QualityPNG
	s.saveHistoryItem(store, pic)
	if len(asked) != 3 || asked[0].DefaultExtension != "jpg" || asked[1].DefaultExtension != "txt" || asked[2].DefaultExtension != "png" {
		t.Fatalf("dialogs = %+v", asked)
	}

	data, err := historyFileData(store, text, 80)
	if err != nil || string(data) != "复制的文字" {
		t.Fatalf("text file = %q %v", data, err)
	}
	stored, _ := store.ImagePNG(pic.ID)
	data, err = historyFileData(store, pic, 0)
	if err != nil || !bytes.Equal(data, stored) {
		t.Fatal("PNG quality writes the stored PNG unchanged")
	}
	data, err = historyFileData(store, pic, 60)
	if err != nil || !isJPEG(data) {
		t.Fatal("a JPEG quality re-encodes")
	}
	if back, err := jpeg.Decode(bytes.NewReader(data)); err != nil || back.Bounds() != image.Rect(0, 0, 64, 32) {
		t.Fatalf("JPEG decodes at the picture's size: %v", err)
	}
}
