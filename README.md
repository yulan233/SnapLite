# SnapLite (简截)

English | [中文](README.zh-CN.md)

A lightweight screenshot tool that lives in the Windows tray and the macOS menu bar. Written in Go, with its interface in [Viva](https://github.com/vivaui/viva) v0.2.1 (native GPU rendering, no WebView).
Requirements, design and implementation notes are in [docs/](docs/README.md) (Chinese).

## Features

- **Region capture**: freezes every display, lets you select an area (with window snapping and an 8× magnifier), then copies it to the clipboard or saves it as PNG / JPEG. With several displays at different scales (say a 100% primary and a 125% secondary) every display is captured edge to edge in its own physical pixels.
- **Pins**: the toolbar's Pin button pins the screenshot where it was taken; images and text from the clipboard history can be pinned too. Pins float above other windows without taking focus, can be dragged, close on double-click, and offer Copy, Save As… and Unpin on right-click; the tray's Close All Pins closes them all. Pins are not saved and close when SnapLite quits.
- **Capture quality**: Settings › Capture offers Original (lossless PNG) / High / Medium / Low (JPEG 92% / 80% / 60%). It only affects saved files: the clipboard always gets lossless PNG. Resolution can be set to “Standard 1×”, which shrinks high-DPI screenshots to their logical size.
- **Annotations**: rectangle, ellipse, arrow, pen and mosaic; six colours, three widths; undo / redo; `Shift` constrains to a square / circle / 45°.
- **Mosaic**: drag to paint over what should not be seen; the painted area turns into blocks, each the average colour of the original picture under it. The three widths set the brush and block sizes. Mosaics always sit under the other annotations, and what you see is what gets exported.
- **Clipboard history**: keeps the latest 100 images and texts (deduplicated, 500 MB cap, kept across restarts) and skips what password managers mark as concealed. The panel is searchable and keyboard driven: clicking an item copies it, and the Paste button, `⌘↵` or `⌘1–9` paste straight into the previous app (macOS needs the Accessibility permission). Right-click a row to Copy / Paste / Pin to Desktop / Save As… / Delete.
- **Interface language**: Chinese or English, in Settings › General › Language. It follows the system by default and switches immediately.
- Long screenshots were removed on 2026-11-01.

## Shortcuts

| Action | macOS | Windows |
| --- | --- | --- |
| Capture | `⌥F1` | `Right Alt+F1` |
| Clipboard history | `⌥F2` | `Right Alt+F2` |
| Done (copy) | `Enter` / double-click the selection / `⌘C` | `Enter` / double-click the selection / `Ctrl+C` |
| Save | `⌘S` | `Ctrl+S` |
| Exit capture / cancel the stroke being drawn | `Esc` / right-click | `Esc` / right-click |
| Undo / redo | `⌘Z` / `⌘⇧Z` | `Ctrl+Z` / `Ctrl+Y`, `Ctrl+Shift+Z` |
| Pick rectangle / ellipse / arrow / pen / mosaic | `⌘1`–`⌘5` | `Ctrl+1`–`Ctrl+5` |
| Copy the colour under the magnifier | `⌘⇧C` | `Ctrl+Shift+C` |
| Nudge the selection | arrow keys (`Shift`: 10 px) | same |
| Close a pin | double-click / `Esc` (while it has focus) | same |

On a Mac keyboard F1–F2 are media keys by default: press `⌥ fn F1`, or turn on “Use F1, F2, etc. keys as standard function keys” in System Settings › Keyboard › Keyboard Shortcuts › Function Keys.

On Windows only the right Alt key works. SnapLite registers Alt+F1 and checks that the right Alt key is the one held when it fires (AltGr layouts also get Ctrl+Alt+F1). While SnapLite runs, left Alt+F1 is therefore taken too and does nothing. This avoids a global keyboard hook (see “Antivirus false positives” below). The global hotkeys can be changed in Settings › Hotkeys or in the config file.

## Building

Needs Go 1.26+. macOS needs the Xcode Command Line Tools (the platform layer is cgo + Objective-C); the Windows build is pure Go and cross-compiles from macOS.

```bash
go build -o build/snaplite ./cmd/snaplite                                  # this machine
GOOS=windows GOARCH=amd64 go build -o build/snaplite.exe ./cmd/snaplite    # Windows x64
GOOS=windows GOARCH=arm64 go build -o build/snaplite-arm64.exe ./cmd/snaplite

./scripts/setup-mac-signing.sh                                              # once per Mac: a local signing certificate (see Permissions)
./scripts/package-mac.sh                                                    # build/简截.app (LSUIElement, screen-recording text, signed)
./scripts/package-mac.sh amd64                                              # Intel Mac build (cross-compiled on Apple silicon)
go run github.com/vivaui/viva/cmd/viva@v0.2.1 package -target windows -arch amd64 -format zip -o build/windows
go run ./cmd/genicon/main.go                                                # regenerate assets/icon.png and tray-template.png
go build -tags probe -o build/platform-probe ./cmd/platform-probe           # platform-layer probe for real hardware (triggers permission prompts)
```

Tests (no window opens; the UI is checked with Viva's `app.Driver`):

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=arm64 go build ./...
```

## Releasing

Pushing a tag that starts with `v` runs [`.github/workflows/release.yml`](.github/workflows/release.yml) on a macOS runner: it runs the tests, writes the tag into `viva.toml`'s version, builds four zips (macOS arm64 / x64, Windows x64 / arm64) and creates a GitHub Release with generated notes:

```bash
git tag v0.2.0 && git push origin v0.2.0
```

Release builds are not properly signed: on macOS right-click › Open the downloaded app or run `xattr -dr com.apple.quarantine 简截.app`; Windows shows a SmartScreen prompt. For real signing, `viva package` reads environment variables (`APPLE_CERTIFICATE`, `APPLE_ID` and so on for signing and notarization, `WINDOWS_CERTIFICATE` and so on for Windows; see `viva help package`) that can go into the repository's secrets; `scripts/package-mac.sh` also signs with the identity named by `SNAPLITE_SIGN_IDENTITY`.

## Permissions (macOS)

- **Screen Recording**: macOS asks on the first capture. If it is denied, SnapLite explains and can open System Settings › Privacy & Security › Screen Recording. Restart SnapLite after granting it.
  macOS ties this permission to the code signature, and an ad hoc signature changes with every build, so each repackage used to need the permission granted again. Run `./scripts/setup-mac-signing.sh` once on your Mac: it creates the self-signed certificate “SnapLite Local Signing” in the login keychain and trusts it for code signing (macOS asks for your password once). `package-mac.sh` then signs with it, so one grant survives every rebuild. CI release builds are still signed ad hoc and need the permission again after an upgrade.
- **Accessibility**: only needed by the clipboard history's “paste into the previous app”. Without it the item is still copied, and macOS shows its permission prompt.
- The packaged `.app` sets `LSUIElement`, so it has no Dock icon; when `build/snaplite` runs bare, the program switches its activation policy to Accessory.

Windows needs no extra permissions. Pasting into a window that runs as administrator is not possible (UIPI); the item is only copied then.

## Antivirus false positives (Windows)

Screen capture, clipboard reading, synthetic key presses and launch at login, in an unsigned program, is what heuristic engines take for a keylogger; 360 reported SnapLite as `Trojan.Generic`. What has been done:

- The global low-level keyboard hook (`SetWindowsHookEx(WH_KEYBOARD_LL)`) is gone; the right-Alt hotkeys use `RegisterHotKey`.
- The exe carries version information (company, description, version, icon); see `[windows]` in `viva.toml`.

The lasting fix is an Authenticode code-signing certificate for the exe, and submitting the signed build to 360's developer platform (open.soft.360.cn) for whitelisting / a false-positive report.

## Configuration and data

Data directory: `~/Library/Application Support/SnapLite` on macOS, `%APPDATA%\SnapLite` on Windows.

- `config.json`: written with the defaults on first launch. It holds the hotkeys (such as `"Option+F1"`, `"RightAlt+F1"`, `"CmdOrCtrl+Shift+A"`; keys are letters, digits and F1–F12; defaults written by older versions, including the clipboard history's former `Option+F3` / `RightAlt+F3`, are replaced with the new defaults), `saveDir`, the history's item and byte limits, whether text is recorded, what clicking a history item does (`history.clickAction`: `copy` / `paste`), the capture quality (`capture.quality`: `png` / `high` / `medium` / `low`), the resolution (`capture.scale`: `original` / `1x`) and the interface language (`language`: empty follows the system, `zh` / `en`). The old `longshot` field disappears on the next save. Most of these can also be changed in the tray's Settings….
- `history/`: `index.json` and the images as PNG.
- `snaplite.log`: errors and crash logs (about 1 MB is kept).

## Known limitations

- Real window behaviour (the overlay staying on top and covering the menu bar / taskbar, getting keyboard focus at once, several displays at mixed scales, permission prompts, real Windows machines) is only verified through `app.Driver` and cross-compilation; check it on hardware following [docs/07](docs/07-测试与验收.md) §3.
- Viva v0.2.1's save dialog cannot set an initial folder, so it opens where the system remembers.
- Viva does not give unmodified letters and digits to non-text windows, so picking tools and copying colours use `⌘/Ctrl` chords (see the table).
- On Windows ↑↓ move the caret while the history panel's search box has focus (Viva's `NavigationKeys` is macOS-only for now); `Tab` moves focus to the list.
- macOS has no clipboard change notification, so the history checks with a slow 800 ms timer (the only standing timer); Windows is event driven.
- The toolbar's 120 ms fade-in is not implemented (it appears at once).
- Pins staying on top, dragging, double-clicking and the right-click menu are only verified through `app.Driver`; on macOS clicking a pin activates SnapLite (so the pin can receive `Esc`).
- After switching the language, open windows keep their old title until they are opened again.
- Selections across displays, screen recording, OCR and long screenshots are out of scope.
