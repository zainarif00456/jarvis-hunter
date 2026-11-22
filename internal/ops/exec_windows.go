//go:build windows

package ops

import (
	"os/exec"
	"strconv"
	"syscall"
)

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd.exe")
	// pass the line untouched: cmd.exe has its own quoting rules
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /D /S /C "` + command + `"`}
	return cmd
}

func prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

func killTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
