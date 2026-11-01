package shell

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	vui "github.com/vivaui/viva/ui"

	"snaplite/internal/platform"
	sui "snaplite/internal/ui"
)

var (
	permissionFeature = sui.Tree.Feature("permission")
	permissionOpen    = sui.Tree.ID(permissionFeature, "open-settings")
	permissionClose   = sui.Tree.ID(permissionFeature, "close")
)

// PermissionText explains the missing screen-recording permission (docs/03 §5).
const PermissionText = "需要在 系统设置 › 隐私与安全性 › 屏幕录制 中允许简截，授权后请重新启动简截。"

// permissionConfig is the explanation window. openSettings and close are
// injected so a Driver test can run it.
func permissionConfig(tokens sui.Tokens, openSettings, close func()) app.Config {
	var view flex.View
	theme := tokens.VivaTheme()
	config := app.Config{Title: "简截 · 需要屏幕录制权限", Width: 440, Height: 180, Tree: &sui.Tree}
	config.Paint = func(canvas *vui.Canvas, size vui.Size) {
		bounds := vui.Rect{Width: size.Width, Height: size.Height}
		canvas.Fill(bounds, tokens.Surface)
		view.Draw(canvas, theme, bounds, func(f *flex.Frame) {
			f.Column().Fill().Pad(24).Gap(16).Children(func() {
				f.Text("简截需要屏幕录制权限").Bold()
				f.Wrap(PermissionText).Muted()
				f.Row().Gap(8).Justify(flex.End).Children(func() {
					if f.Button(permissionClose, "好").Outline().Clicked() {
						canvas.AfterPaint(close)
					}
					if f.Button(permissionOpen, "打开系统设置").Clicked() {
						canvas.AfterPaint(openSettings)
					}
				})
			})
		})
	}
	return config
}

func (s *Shell) showPermission() {
	if s.permission != nil && !s.permission.Closed() {
		s.permission.Show()
		return
	}
	platform.RequestScreenCapturePermission()
	var w *app.Window
	config := permissionConfig(sui.Current(),
		func() { platform.OpenPrivacySettings("screen") },
		func() {
			if w != nil {
				w.Close()
			}
		})
	w, err := s.app.OpenWindow(config, new(vui.Canvas))
	if err != nil {
		s.log.Printf("permission window: %v", err)
		return
	}
	s.permission = w
	platform.ActivateApp()
}
