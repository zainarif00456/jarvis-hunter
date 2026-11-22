#!/usr/bin/env bash
# Lay out a USB stick (or any folder): one folder per OS with the binary and a double-click
# launcher, so Zain can plug it into any computer. Run scripts/build.sh first.
#
#   scripts/make-usb.sh /media/zain/USBSTICK     (creates JarvisSupportHost/ inside it)
#
# Optional: drop a static ffmpeg for each OS into usb-template/ffmpeg/<os>/ (ffmpeg or ffmpeg.exe)
# before running this, to get camera support on machines without ffmpeg installed.
set -euo pipefail
cd "$(dirname "$0")/.."
DEST="${1:?usage: make-usb.sh <destination folder>}/JarvisSupportHost"
[ -d dist ] || { echo "run scripts/build.sh first"; exit 1; }
rm -rf "$DEST"; mkdir -p "$DEST"/{mac,windows,linux}

put() { # put <dist file> <dest dir> <dest name>
  if [ -f "dist/$1" ]; then cp "dist/$1" "$2/$3"; chmod +x "$2/$3" 2>/dev/null || true; else echo "  (missing dist/$1: skipped)"; fi
}
if [ -f dist/jarvis-host-darwin-universal ]; then put jarvis-host-darwin-universal "$DEST/mac" jarvis-host
else put jarvis-host-darwin-arm64 "$DEST/mac" jarvis-host-arm64; put jarvis-host-darwin-amd64 "$DEST/mac" jarvis-host-intel; fi
put jarvis-host-windows-amd64.exe "$DEST/windows" jarvis-host.exe
put jarvis-host-windows-arm64.exe "$DEST/windows" jarvis-host-arm64.exe
put jarvis-host-linux-amd64 "$DEST/linux" jarvis-host
put jarvis-host-linux-arm64 "$DEST/linux" jarvis-host-arm64

cp usb-template/Start-on-Mac.command "$DEST/mac/"; cp usb-template/Start-on-Windows.bat "$DEST/windows/"
cp usb-template/start-on-linux.sh "$DEST/linux/"; cp usb-template/README-USB.txt "$DEST/"
chmod +x "$DEST/mac/Start-on-Mac.command" "$DEST/linux/start-on-linux.sh"
for os in mac windows linux; do
  src="usb-template/ffmpeg/$os"
  if [ -d "$src" ]; then mkdir -p "$DEST/$os/bin" && cp -r "$src"/* "$DEST/$os/bin/"; fi
done
[ -f dist/SHA256SUMS ] && cp dist/SHA256SUMS "$DEST/"
echo "USB layout ready: $DEST"; find "$DEST" -maxdepth 2 | sort
