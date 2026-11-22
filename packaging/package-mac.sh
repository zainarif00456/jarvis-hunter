#!/usr/bin/env bash
# macOS only (needs lipo, codesign, hdiutil): turn dist/jarvis-host-darwin-universal into
# dist/JarvisSupportHost-<version>-mac-universal.dmg containing "Jarvis Support Host.app".
# The app opens Terminal and runs the host there, so the person sees the console and can type the
# server address the first time. Ad-hoc signed only (Apple Silicon refuses code with NO signature);
# real Developer ID signing + notarization goes in scripts/sign.sh (see the placeholder there).
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:?set VERSION}"
BIN=dist/jarvis-host-darwin-universal
[ -f "$BIN" ] || { echo "missing $BIN (run scripts/build.sh darwin/arm64 darwin/amd64 on a Mac)"; exit 1; }
APP="dist/dmg/Jarvis Support Host.app"
rm -rf dist/dmg && mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/Resources/jarvis-host"; chmod +x "$APP/Contents/Resources/jarvis-host"
cat > "$APP/Contents/MacOS/launcher" <<'SH'
#!/bin/bash
DIR="$(cd "$(dirname "$0")/../Resources" && pwd)"
osascript -e "tell application \"Terminal\" to do script \"clear; '$DIR/jarvis-host'\"" -e 'tell application "Terminal" to activate'
SH
chmod +x "$APP/Contents/MacOS/launcher"
cat > "$APP/Contents/Info.plist" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>Jarvis Support Host</string>
<key>CFBundleIdentifier</key><string>com.jarvis.supporthost</string>
<key>CFBundleExecutable</key><string>launcher</string>
<key>CFBundleVersion</key><string>${VERSION}</string>
<key>CFBundleShortVersionString</key><string>${VERSION}</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSMinimumSystemVersion</key><string>11.0</string>
<key>NSCameraUsageDescription</key><string>Jarvis can use this computer's camera when you have connected it.</string>
</dict></plist>
PL
# ad-hoc signature (identity "-"): NOT a developer signature. Replace via scripts/sign.sh later.
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
lipo -archs "$APP/Contents/Resources/jarvis-host"
ln -s /Applications dist/dmg/Applications
hdiutil create -volname "Jarvis Support Host" -srcfolder dist/dmg -ov -format UDZO "dist/JarvisSupportHost-${VERSION}-mac-universal.dmg"
rm -rf dist/dmg
ls -lh dist/*.dmg
