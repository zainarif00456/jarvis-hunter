//go:build !windows

package ui

import "os/exec"

func hide(_ *exec.Cmd) {}
