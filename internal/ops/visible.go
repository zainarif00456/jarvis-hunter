package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// execVisible runs the command in a terminal window on the host's own screen, so the person at
// the machine can see it and type any sudo/admin password there. Jarvis never types it. The
// output and exit code are collected through files in a private temp folder.
func execVisible(ctx context.Context, a execArgs, cwd string) (Result, error) {
	dir, err := os.MkdirTemp("", "jarvis-run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out, code := filepath.Join(dir, "out.txt"), filepath.Join(dir, "code.txt")
	script, err := visibleScript(dir, a.Command, cwd, out, code)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if err := openWindow(script); err != nil {
		return nil, errors.New("could not open a terminal window on the host: " + err.Error())
	}
	timeout := time.Duration(a.TimeoutS * float64(time.Second))
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.After(timeout)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("cancelled")
		case <-deadline:
			return Result{"timed_out": true, "exit_code": nil, "duration_ms": time.Since(start).Milliseconds(),
				"cwd": workdir(cwd), "output": readTail(out)}, nil
		case <-tick.C:
			raw, err := os.ReadFile(code)
			if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
				continue
			}
			n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
			return Result{"exit_code": n, "duration_ms": time.Since(start).Milliseconds(),
				"cwd": workdir(cwd), "timed_out": false, "output": readTail(out)}, nil
		}
	}
}

func readTail(path string) string {
	raw, _ := os.ReadFile(path)
	if len(raw) > 200_000 {
		raw = raw[len(raw)-200_000:]
	}
	return utf8(raw)
}
