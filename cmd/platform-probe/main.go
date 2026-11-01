//go:build probe

// Command platform-probe exercises the native platform layer on a real
// machine: it lists displays, captures the primary display to a temporary
// PNG, waits five seconds for Cmd/Ctrl+Shift+9 and writes a test image to the
// clipboard. It opens no visible window but may trigger permission prompts.
// Build with: go build -tags probe ./cmd/platform-probe
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/app"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
)

func main() {
	err := app.RunApplication(func(a *app.Application) error {
		var canvas vui.Canvas
		host, err := a.OpenWindow(app.Config{Title: "snaplite-probe", WindowMode: app.WindowFrameless, Width: 1, Height: 1}, &canvas)
		if err != nil {
			return err
		}
		host.Hide()
		if err := platform.Init(); err != nil {
			log.Printf("init: %v", err)
		}
		displays, err := platform.Displays()
		fmt.Printf("displays: %+v err=%v\n", displays, err)
		fmt.Printf("screen permission=%v accessibility=%v\n", platform.HasScreenCapturePermission(), platform.HasAccessibilityPermission())
		go func() {
			for _, d := range displays {
				if !d.Primary {
					continue
				}
				start := time.Now()
				img, err := platform.CaptureDisplay(d.ID)
				if err != nil {
					log.Printf("capture: %v", err)
					break
				}
				path := filepath.Join(os.TempDir(), "snaplite-probe.png")
				f, _ := os.Create(path)
				_ = png.Encode(f, img)
				_ = f.Close()
				fmt.Printf("captured %v in %v -> %s\n", img.Bounds().Size(), time.Since(start), path)
			}
		}()
		unregister, err := platform.RegisterHotkey(platform.Hotkey{Mods: platform.ModCmdOrCtrl | platform.ModShift, Key: '9'}, func() {
			fmt.Println("hotkey pressed")
		})
		fmt.Printf("hotkey registered err=%v; press Cmd/Ctrl+Shift+9 within 5 s\n", err)
		test := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for i := 0; i < 64*64; i++ {
			test.Set(i%64, i/64, color.RGBA{uint8(i % 64 * 4), 120, 200, 255})
		}
		fmt.Printf("clipboard image err=%v count=%d\n", platform.SetClipboardImage(test), platform.ClipboardChangeCount())
		time.AfterFunc(5*time.Second, func() {
			platform.PostMain(func() {
				if unregister != nil {
					unregister()
				}
				a.Quit()
			})
		})
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
