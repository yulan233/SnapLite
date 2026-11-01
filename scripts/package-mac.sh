#!/bin/sh
# Builds build/简截.app with viva package, then fixes what viva.toml cannot
# express: [mac.info] only writes strings, and LSUIElement should be a
# boolean. Finally signs ad hoc so the screen-recording permission sticks to
# this build (macOS ties the permission to the code signature).
# Usage: package-mac.sh [arm64|amd64]  (default: arm64)
set -eu
cd "$(dirname "$0")/.."
ARCH="${1:-arm64}"
APP="build/简截.app"
rm -rf "$APP"
go run github.com/vivaui/viva/cmd/viva@v0.2.1 package -arch "$ARCH" -o "$APP" -no-sign
plutil -replace LSUIElement -bool true "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"
codesign --force --deep -s - "$APP"
echo "built $APP"
