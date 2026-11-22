// Package ui is everything the person at this computer sees: the console window (which Jarvis can
// show or hide), and the on-screen indicator with a Disconnect button while Jarvis is connected.
// None of it limits what Jarvis can do; it only keeps the person informed and in charge.
package ui

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

var quiet atomic.Bool

// Logf prints a timestamped line to the console unless it is hidden.
func Logf(format string, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprintf(os.Stdout, "%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// Banner prints a block that is always meant to be seen.
func Banner(lines ...string) {
	if quiet.Load() {
		return
	}
	fmt.Println()
	for _, l := range lines {
		fmt.Println("  " + l)
	}
	fmt.Println()
}

// SetConsoleVisible shows or hides the console. On Windows the window really hides; on macOS and
// Linux the app runs in the terminal that started it, so "hidden" mutes its output (use
// --background to detach it from the terminal entirely). It returns a description of what it did.
func SetConsoleVisible(visible bool) string {
	mode := setConsole(visible)
	quiet.Store(!visible)
	if visible {
		Logf("console shown")
	}
	return mode
}

// Visible reports whether the console is currently shown.
func Visible() bool { return !quiet.Load() }
