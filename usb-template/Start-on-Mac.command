#!/bin/bash
# Double-click me. (First time: right-click > Open, because this app is not signed yet.)
cd "$(dirname "$0")"
# the app is unsigned: remove the "downloaded from the internet" flag so macOS lets it run
xattr -dr com.apple.quarantine . 2>/dev/null
case "$(uname -m)" in arm64) BIN=./jarvis-host-arm64 ;; *) BIN=./jarvis-host-intel ;; esac
[ -x ./jarvis-host ] && BIN=./jarvis-host     # a universal build
exec "$BIN" "$@"
