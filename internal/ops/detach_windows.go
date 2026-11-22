//go:build windows

package ops

import (
	"os/exec"
	"syscall"
)

func detach(_ *exec.Cmd) {}
func setCmdLine(cmd *exec.Cmd, line string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line, HideWindow: true}
}
