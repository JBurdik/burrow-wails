#!/bin/sh
# Wrap the release binary in a minimal .app so it gets a Dock name and can be opened from Finder.
set -e
cd "$(dirname "$0")"
swift build -c release
APP=build/BurrowSwift.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp .build/release/BurrowSwift "$APP/Contents/MacOS/"
cp -R .build/release/BurrowSwift_BurrowSwift.bundle "$APP/Contents/Resources/"
cp ../src-wails/build/darwin/icons.icns "$APP/Contents/Resources/AppIcon.icns" 2>/dev/null || true
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Burrow Swift</string>
  <key>CFBundleIdentifier</key><string>dev.burrow.swift-experiment</string>
  <key>CFBundleExecutable</key><string>BurrowSwift</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
echo "$PWD/$APP"
