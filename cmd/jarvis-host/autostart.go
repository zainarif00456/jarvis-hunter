package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// autostart installs or removes "start at login, in the background" the way each OS does it.
func autostart(install bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		plist := filepath.Join(home, "Library", "LaunchAgents", "com.jarvis.supporthost.plist")
		if !install {
			_ = exec.Command("launchctl", "unload", plist).Run()
			return removeFile(plist)
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.jarvis.supporthost</string>
<key>ProgramArguments</key><array><string>%s</string><string>--hidden</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
</dict></plist>
`, exe)
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
			return err
		}
		return exec.Command("launchctl", "load", plist).Run()
	case "windows":
		if !install {
			return exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "JarvisSupportHost", "/f").Run()
		}
		return exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "JarvisSupportHost",
			"/t", "REG_SZ", "/d", `"`+exe+`" --background`, "/f").Run()
	default:
		unit := filepath.Join(home, ".config", "systemd", "user", "jarvis-support-host.service")
		if !install {
			_ = exec.Command("systemctl", "--user", "disable", "--now", "jarvis-support-host.service").Run()
			return removeFile(unit)
		}
		body := fmt.Sprintf("[Unit]\nDescription=Jarvis Support Host\nAfter=network-online.target\n\n[Service]\nExecStart=%s --hidden\nRestart=on-failure\nRestartSec=10\n\n[Install]\nWantedBy=default.target\n", exe)
		if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
			return err
		}
		if err := exec.Command("systemctl", "--user", "enable", "--now", "jarvis-support-host.service").Run(); err != nil {
			return errors.New("wrote " + unit + " but systemctl --user failed: " + err.Error())
		}
		return nil
	}
}

func removeFile(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
