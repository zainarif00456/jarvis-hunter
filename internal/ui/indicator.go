package ui

import (
	"context"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Indicator is the on-screen "Jarvis is connected" notice with a Disconnect button. It is a small
// native window made by the OS's own scripting (AppleScript, PowerShell, zenity/kdialog), so the
// app stays one dependency-free binary. If none can open (no display), the console banner is the
// indicator and Stop is Ctrl+C or typing "stop".
type Indicator struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	pressed chan struct{}
	shown   bool
}

// NewIndicator makes one (not yet shown).
func NewIndicator() *Indicator { return &Indicator{pressed: make(chan struct{}, 1)} }

// Pressed receives once when the person presses Disconnect.
func (i *Indicator) Pressed() <-chan struct{} { return i.pressed }

// Show puts the window up (idempotent). text describes who is connected.
func (i *Indicator) Show(text string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.shown {
		return
	}
	cmd, ok := indicatorCommand(text)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	hide(proc)
	started := time.Now()
	if err := proc.Start(); err != nil {
		cancel()
		return
	}
	i.cancel, i.shown = cancel, true
	go func() {
		_ = proc.Wait()
		// A window that vanished at once could not be shown (no display): not a press.
		pressedByUser := ctx.Err() == nil && time.Since(started) > 700*time.Millisecond
		i.mu.Lock()
		i.shown = false
		i.mu.Unlock()
		if pressedByUser {
			select {
			case i.pressed <- struct{}{}:
			default:
			}
		}
	}()
}

// Hide takes the window down (Jarvis disconnected).
func (i *Indicator) Hide() {
	i.mu.Lock()
	cancel := i.cancel
	i.cancel = nil
	i.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func indicatorCommand(text string) ([]string, bool) {
	msg := "Jarvis is connected to this computer.\n" + text + "\n\nThe action log is kept on this computer. Press Disconnect to stop it."
	switch runtime.GOOS {
	case "darwin":
		script := `display dialog ` + appleQuote(msg) + ` with title "Jarvis Support" buttons {"Disconnect"} default button "Disconnect" with icon caution`
		return []string{"osascript", "-e", script}, true
	case "windows":
		return []string{"powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", windowsForm(msg)}, true
	default:
		for _, c := range [][]string{
			{"zenity", "--warning", "--title=Jarvis Support", "--ok-label=Disconnect", "--no-markup", "--text=" + msg},
			{"yad", "--title=Jarvis Support", "--button=Disconnect:0", "--text=" + msg, "--on-top"},
			{"kdialog", "--title", "Jarvis Support", "--msgbox", msg},
		} {
			if p, err := exec.LookPath(c[0]); err == nil {
				return append([]string{p}, c[1:]...), true
			}
		}
	}
	return nil, false
}

func appleQuote(s string) string {
	out := `"`
	for _, r := range s {
		switch r {
		case '"':
			out += `\"`
		case '\\':
			out += `\\`
		case '\n':
			out += `" & return & "`
		default:
			out += string(r)
		}
	}
	return out + `"`
}

func windowsForm(msg string) string {
	esc := ""
	for _, r := range msg {
		switch r {
		case '"':
			esc += "'"
		case '$', '`':
			// would be interpreted by PowerShell inside a double-quoted string: dropped
		case '\n':
			esc += "`n"
		default:
			esc += string(r)
		}
	}
	return `Add-Type -AssemblyName System.Windows.Forms;` +
		`$f=New-Object System.Windows.Forms.Form;$f.Text='Jarvis Support';$f.TopMost=$true;` +
		`$f.Width=440;$f.Height=210;$f.StartPosition='CenterScreen';$f.BackColor=[System.Drawing.Color]::MistyRose;` +
		`$l=New-Object System.Windows.Forms.Label;$l.Text="` + esc + `";$l.Left=14;$l.Top=12;$l.Width=400;$l.Height=100;` +
		`$b=New-Object System.Windows.Forms.Button;$b.Text='Disconnect';$b.Left=14;$b.Top=125;$b.Width=140;` +
		`$b.Add_Click({$f.Close()});$f.Controls.Add($l);$f.Controls.Add($b);` +
		`$f.ShowDialog()|Out-Null`
}
