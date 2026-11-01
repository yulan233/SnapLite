package shell

import (
	"testing"

	vui "github.com/vivaui/viva/ui"
)

func TestSettingsFitsWindow(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		r := newSettingsRig(t, goos, false)
		r.click(settingsRows[0])
		if err := r.d.Key(vui.KeyF2, vui.ModifierAlt); err != nil { // a refusal adds a message line
			t.Fatal(err)
		}
		snap := r.d.Snapshot()
		for _, h := range snap.Hits {
			if h.Bounds.Y+h.Bounds.Height > float32(snap.Height) || h.Bounds.X+h.Bounds.Width > float32(snap.Width) {
				t.Errorf("%s: %s at %+v spills out of %dx%d", goos, h.Label, h.Bounds, snap.Width, snap.Height)
			}
		}
	}
}
