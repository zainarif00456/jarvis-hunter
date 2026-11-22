//go:build !windows

package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func shellCommand(command string) *exec.Cmd {
	sh := os.Getenv("SHELL")
	switch filepath.Base(sh) {
	case "bash", "zsh":
		return exec.Command(sh, "-lc", command) // login shell: the user's own PATH (Homebrew etc.)
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return exec.Command("/bin/bash", "-c", command)
	}
	return exec.Command("/bin/sh", "-c", command)
}

func prepare(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func killTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
