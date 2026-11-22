//go:build !windows

package term

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/creack/pty"
)

type unixPTY struct {
	f    *os.File
	cmd  *exec.Cmd
	once sync.Once
}

// Open starts the user's shell on a new PTY.
func Open(cwd string, cols, rows int) (PTY, string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		for _, c := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
			if _, err := os.Stat(c); err == nil {
				shell = c
				break
			}
		}
	}
	args := []string{}
	if b := filepath.Base(shell); b == "bash" || b == "zsh" || b == "fish" {
		args = append(args, "-l")
	}
	cmd := exec.Command(shell, args...)
	dir := ExpandCwd(cwd)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "JARVIS_SUPPORT_HOST=1")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, "", err
	}
	return &unixPTY{f: f, cmd: cmd}, dir, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
func (p *unixPTY) Wait() int {
	_ = p.cmd.Wait()
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode()
	}
	return -1
}
func (p *unixPTY) Close() error {
	var err error
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(os.Interrupt)
			_ = p.cmd.Process.Kill()
		}
		err = p.f.Close()
	})
	return err
}
