#!/usr/bin/env bash
# Linux: for each arch in dist/, make a .deb (installs /usr/bin/jarvis-host) and a .tar.gz.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:?set VERSION}"
DEBVER="${VERSION#v}"
for arch in amd64 arm64; do
  bin="dist/jarvis-host-linux-$arch"; [ -f "$bin" ] || continue
  root="dist/deb-$arch"; rm -rf "$root"; mkdir -p "$root/DEBIAN" "$root/usr/bin" "$root/usr/share/doc/jarvis-support-host"
  install -m 0755 "$bin" "$root/usr/bin/jarvis-host"
  cp README.md "$root/usr/share/doc/jarvis-support-host/README.md"
  cat > "$root/DEBIAN/control" <<CTL
Package: jarvis-support-host
Version: ${DEBVER}
Section: utils
Priority: optional
Architecture: ${arch}
Maintainer: Jarvis Support Host <noreply@users.noreply.github.com>
Recommends: ffmpeg, zenity
Description: Jarvis Support Host
 Lets your Jarvis assistant look after this computer. Connects outward only.
 Run: jarvis-host https://your-jarvis-address
CTL
  dpkg-deb --build --root-owner-group "$root" "dist/jarvis-support-host_${DEBVER}_${arch}.deb"
  tar -C dist -czf "dist/jarvis-host-${DEBVER}-linux-${arch}.tar.gz" --transform "s|jarvis-host-linux-$arch|jarvis-host|" "jarvis-host-linux-$arch"
  rm -rf "$root"
done
ls -lh dist
