#!/usr/bin/env bash
# Build the macOS spike (debug and release) and assemble an ad-hoc signed
# bundle macos/.build/bundle/TotalRecall.app from the release binary. No
# Developer ID, no notarization (see the PR of #317 for the plan).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
bash "$ROOT/macos/scripts/check.sh"
cd "$ROOT/macos"
swift build -c debug
swift build -c release
BIN=$(swift build -c release --show-bin-path)/TotalRecall
BUNDLE=$ROOT/macos/.build/bundle/TotalRecall.app
VERSION=$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo 0.0.0)
BUILD=$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 1)
rm -rf "$ROOT/macos/.build/bundle"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp "$BIN" "$BUNDLE/Contents/MacOS/TotalRecall"
cat > "$BUNDLE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>io.github.seletz.total-recall</string>
  <key>CFBundleName</key><string>Total Recall</string>
  <key>CFBundleDisplayName</key><string>Total Recall</string>
  <key>CFBundleExecutable</key><string>TotalRecall</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION:-0.0.0}</string>
  <key>CFBundleVersion</key><string>$BUILD</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
plutil -lint "$BUNDLE/Contents/Info.plist" >/dev/null
codesign --force --sign - --options runtime "$BUNDLE"
codesign --verify --strict "$BUNDLE"
echo "macos: bundle $BUNDLE (ad-hoc signed)"
