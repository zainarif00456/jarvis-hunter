//go:build windows

package ui

import "syscall"

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindow       = user32.NewProc("ShowWindow")
)

func setConsole(visible bool) string {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return "no console window to change"
	}
	cmd := uintptr(0) // SW_HIDE
	if visible {
		cmd = 5 // SW_SHOW
	}
	showWindow.Call(hwnd, cmd)
	return "console window " + map[bool]string{true: "shown", false: "hidden"}[visible]
}
