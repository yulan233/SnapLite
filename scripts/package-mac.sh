#!/bin/sh
# Builds build/简截.app with viva package, then fixes what viva.toml cannot
# express: [mac.info] only writes strings, and LSUIElement should be a
# boolean. Finally signs it. macOS ties the screen-recording permission to the
# code signature: with the stable certificate setup-mac-signing.sh creates
# (or SNAPLITE_SIGN_IDENTITY) the permission survives rebuilds; the ad hoc
# fallback changes with every build, so the permission must be granted again.
# Usage: package-mac.sh [arm64|amd64]  (default: arm64)
set -eu
cd "$(dirname "$0")/.."
ARCH="${1:-arm64}"
APP="build/简截.app"
IDENTITY="${SNAPLITE_SIGN_IDENTITY:-SnapLite Local Signing}"
rm -rf "$APP"
go run github.com/vivaui/viva/cmd/viva@v0.2.1 package -arch "$ARCH" -o "$APP" -no-sign
plutil -replace LSUIElement -bool true "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"
if security find-identity -v -p codesigning 2>/dev/null | grep -qF "\"$IDENTITY\""; then
	codesign --force --deep -s "$IDENTITY" "$APP"
	echo "signed with \"$IDENTITY\""
else
	codesign --force --deep -s - "$APP"
	echo "signed ad hoc: run scripts/setup-mac-signing.sh once to keep the screen-recording permission across builds"
fi
echo "built $APP"
