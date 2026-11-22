//go:build !windows

package ops

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd)             { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func setCmdLine(_ *exec.Cmd, _ string) {}
