//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func relaunchDetached(args []string, dir string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "host.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, append([]string{"--child"}, args...)...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}
