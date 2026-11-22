//go:build !windows

package ui

func setConsole(visible bool) string {
	if visible {
		return "console output on"
	}
	return "console output muted (the app keeps running in this terminal; start it with --background to detach)"
}
