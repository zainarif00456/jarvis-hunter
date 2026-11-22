#!/usr/bin/env bash
# Run everything: Go unit/agent tests, then the end-to-end test (real host binary vs a throwaway
# instance of the real Jarvis server code on a spare port; never the live one).
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$HOME/toolchains/go/bin:$PATH"
SRC="${JARVIS_SERVER_SRC:-$HOME/Documents/Projects/project-jarvis-support-host}"   # branch feat/support-host
PY="${JARVIS_PYTHON:-$HOME/miniconda3/envs/jarvis/bin/python}"
echo "== go vet + tests"; go vet ./... && go test -count=1 ./...
echo "== cross-compile check"
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64 windows/arm64; do
  CGO_ENABLED=0 GOOS="${t%/*}" GOARCH="${t#*/}" go build -o /dev/null ./cmd/jarvis-host && echo "  $t ok"
done
echo "== end-to-end"; scripts/build.sh linux/amd64 >/dev/null
JARVIS_SERVER_SRC="$SRC" PYTHONPATH="$SRC/server:$SRC" "$PY" -m pytest e2e -v -p no:cacheprovider
