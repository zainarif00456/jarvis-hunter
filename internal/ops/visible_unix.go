//go:build !windows

package ops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func visibleScript(dir, command, cwd, out, code string) (string, error) {
	inner := filepath.Join(dir, "cmd.sh")
	if err := os.WriteFile(inner, []byte(command+"\n"), 0o700); err != nil {
		return "", err
	}
	runner := filepath.Join(dir, "run.sh")
	cd := ""
	if cwd != "" {
		cd = "cd " + shQuote(cwd) + " || exit 1\n"
	}
	body := fmt.Sprintf("#!/bin/sh\necho '--- Jarvis is running this command on your computer ---'\ncat %s\necho '----------------------------------------------------'\n%s"+
		"( sh %s; echo $? > %s ) 2>&1 | tee %s\necho '--- finished: you can close this window ---'\nsleep 3\n",
		shQuote(inner), cd, shQuote(inner), shQuote(code), shQuote(out))
	return runner, os.WriteFile(runner, []byte(body), 0o700)
}

func openWindow(script string) error {
	if runtime.GOOS == "darwin" {
		apple := `tell application "Terminal" to do script "sh ` + strings.ReplaceAll(script, `"`, `\"`) + `"`
		if err := exec.Command("osascript", "-e", apple, "-e", `tell application "Terminal" to activate`).Run(); err != nil {
			return err
		}
		return nil
	}
	cands := [][]string{
		{"x-terminal-emulator", "-e", "sh", script}, {"gnome-terminal", "--", "sh", script},
		{"konsole", "-e", "sh", script}, {"xfce4-terminal", "-e", "sh " + script},
		{"kitty", "sh", script}, {"alacritty", "-e", "sh", script}, {"xterm", "-e", "sh", script},
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c[0]); err == nil {
			return exec.Command(p, c[1:]...).Start()
		}
	}
	return errors.New("no terminal emulator found (is a desktop session running?)")
}
