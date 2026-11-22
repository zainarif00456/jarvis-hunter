package ops

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type execArgs struct {
	Command  string  `json:"command"`
	Cwd      string  `json:"cwd"`
	TimeoutS float64 `json:"timeout_s"`
	Visible  bool    `json:"visible"`
}

// Exec runs a shell command, streaming stdout and stderr as events. Cancelling ctx (the server
// cancelled, or the connection dropped) kills the whole process tree.
func Exec(ctx context.Context, s Sender, id string, raw json.RawMessage) (Result, error) {
	var a execArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Command) == "" {
		return nil, errors.New("no command given")
	}
	cwd := expandHome(a.Cwd)
	if cwd != "" {
		if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
			return nil, errors.New("working directory does not exist: " + cwd)
		}
	}
	if a.Visible {
		return execVisible(ctx, a, cwd)
	}
	timeout := time.Duration(a.TimeoutS * float64(time.Second))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(a.Command)
	cmd.Dir = cwd
	cmd.Stdin = nil // closed: a command that wants a password fails fast instead of hanging
	prepare(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	pump := func(r io.Reader, name string) {
		defer wg.Done()
		br := bufio.NewReaderSize(r, 32<<10)
		buf := make([]byte, 32<<10)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				s.Event(id, map[string]any{"kind": "out", "stream": name, "data": utf8(buf[:n])})
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout, "stdout")
	go pump(stderr, "stderr")
	done := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			killTree(cmd)
		case <-done:
		}
	}()
	wg.Wait()
	waitErr := cmd.Wait()
	close(done)

	res := Result{"duration_ms": time.Since(start).Milliseconds(), "cwd": workdir(cmd.Dir),
		"timed_out": errors.Is(runCtx.Err(), context.DeadlineExceeded)}
	if cmd.ProcessState != nil {
		res["exit_code"] = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return nil, errors.New("cancelled")
	}
	var ee *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &ee) && cmd.ProcessState == nil {
		return nil, waitErr
	}
	return res, nil
}

func workdir(dir string) string {
	if dir != "" {
		return dir
	}
	d, _ := os.Getwd()
	return d
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}
