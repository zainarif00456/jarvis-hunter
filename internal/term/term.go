// Package term is the interactive terminal: a PTY on macOS/Linux, a ConPTY on Windows.
package term

import (
	"os"
	"path/filepath"
	"strings"
)

// PTY is one pseudo-terminal running a shell.
type PTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	// Wait blocks until the shell exits and returns its exit code.
	Wait() int
	Close() error
}

// ExpandCwd turns "", "~" and "~/x" into a real folder that exists (falling back to home).
func ExpandCwd(cwd string) string {
	home, _ := os.UserHomeDir()
	if cwd == "" || cwd == "~" {
		return home
	}
	if strings.HasPrefix(cwd, "~/") || strings.HasPrefix(cwd, `~\`) {
		cwd = filepath.Join(home, cwd[2:])
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return home
	}
	return cwd
}
