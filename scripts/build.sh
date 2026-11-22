#!/usr/bin/env bash
# Build Jarvis Support Host for every platform from any machine with Go (no C compiler needed:
# CGO is off, so cross-compiling is a plain `go build`).
#
#   scripts/build.sh                 all targets -> dist/
#   scripts/build.sh linux/amd64     just one
#   VERSION=1.0.0 scripts/build.sh   stamp a version
#
# Output names: dist/jarvis-host-<os>-<arch>[.exe], plus dist/jarvis-host-darwin-universal when
# `lipo` exists (i.e. building on a Mac). Checksums: dist/SHA256SUMS.
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="$HOME/toolchains/go/bin:$PATH"
command -v go >/dev/null || { echo "Go is not installed (https://go.dev/dl/)"; exit 1; }

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)}"
TARGETS=("$@")
[ ${#TARGETS[@]} -gt 0 ] || TARGETS=(linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64 windows/arm64)
mkdir -p dist

# ---------------------------------------------------------------------------------------------
# CODE SIGNING HOOK. Zain's decision: no signing for now. When the app goes public, implement the
# body of this function (scripts/sign.sh has the notes for each OS) and nothing else changes.
# It is called once per finished binary: $1 = path, $2 = os.
# ---------------------------------------------------------------------------------------------
sign_artifact() {
  if [ -x scripts/sign.sh ]; then scripts/sign.sh "$1" "$2"; fi
}

for target in "${TARGETS[@]}"; do
  os="${target%/*}"; arch="${target#*/}"
  out="dist/jarvis-host-${os}-${arch}"; [ "$os" = windows ] && out="${out}.exe"
  echo "==> $target -> $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X github.com/jarvis-support/host/internal/version.Version=${VERSION}" \
    -o "$out" ./cmd/jarvis-host
  sign_artifact "$out" "$os"
done

if command -v lipo >/dev/null && [ -f dist/jarvis-host-darwin-arm64 ] && [ -f dist/jarvis-host-darwin-amd64 ]; then
  echo "==> universal macOS binary"
  lipo -create -output dist/jarvis-host-darwin-universal dist/jarvis-host-darwin-arm64 dist/jarvis-host-darwin-amd64
  sign_artifact dist/jarvis-host-darwin-universal darwin
else
  echo "(no lipo here: shipping separate arm64 and amd64 Mac binaries; run this script on a Mac for a universal one)"
fi

( cd dist && (sha256sum jarvis-host-* 2>/dev/null || shasum -a 256 jarvis-host-*) > SHA256SUMS )
ls -lh dist
