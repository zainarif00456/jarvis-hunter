//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

func hide(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
