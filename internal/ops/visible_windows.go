//go:build windows

package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func visibleScript(dir, command, cwd, out, code string) (string, error) {
	script := filepath.Join(dir, "run.cmd")
	cd := ""
	if cwd != "" {
		cd = "cd /d \"" + cwd + "\"\r\n"
	}
	body := "@echo off\r\necho --- Jarvis is running this command on your computer ---\r\necho " +
		strings.ReplaceAll(command, "\n", " ") + "\r\n" + cd +
		"(" + command + ") > \"" + out + "\" 2>&1\r\ntype \"" + out + "\"\r\n" +
		"echo %errorlevel% > \"" + code + "\"\r\necho --- finished ---\r\ntimeout /t 3 >nul\r\n"
	return script, os.WriteFile(script, []byte(body), 0o600)
}

func openWindow(script string) error {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /C start "Jarvis support" cmd.exe /C "` + script + `"`}
	return cmd.Run()
}
