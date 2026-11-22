#!/bin/sh
cd "$(dirname "$0")"
case "$(uname -m)" in aarch64|arm64) exec ./jarvis-host-arm64 "$@" ;; *) exec ./jarvis-host "$@" ;; esac
