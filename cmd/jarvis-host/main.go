// Command jarvis-host is the Jarvis Support Host: drop it on a computer, give it your Jarvis
// server's address once, and Jarvis (the brain, on your own machine) can look after this computer.
// It only ever connects OUTWARD (WSS); it opens no port.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/jarvis-support/host/internal/agent"
	"github.com/jarvis-support/host/internal/audit"
	"github.com/jarvis-support/host/internal/camera"
	"github.com/jarvis-support/host/internal/config"
	"github.com/jarvis-support/host/internal/ui"
	"github.com/jarvis-support/host/internal/version"
)

const usage = `Jarvis Support Host %s

Usage:
  jarvis-host [flags] [SERVER]        connect to a Jarvis server (asked for once, then remembered)
  jarvis-host autostart install       start at login, in the background
  jarvis-host autostart uninstall
  jarvis-host forget                  forget the server and this computer's pass
  jarvis-host version

SERVER is the base address of your Jarvis, e.g. https://jarvis.example.com (also: --server, or
the JARVIS_SERVER environment variable). Nothing is built in; there is no default.

Flags:
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("jarvis-host", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintf(os.Stderr, usage, version.Version); fs.PrintDefaults() }
	server := fs.String("server", "", "server base URL")
	code := fs.String("code", os.Getenv("JARVIS_HOST_CODE"), "one-time invite code from Jarvis (skips waiting for approval)")
	name := fs.String("name", "", "name shown to Jarvis (default: this computer's name)")
	background := fs.Bool("background", false, "detach from the terminal and run quietly")
	hidden := fs.Bool("hidden", false, "start with the console hidden/muted")
	show := fs.Bool("show", false, "start with the console visible (overrides a saved 'hidden')")
	noIndicator := fs.Bool("no-indicator", false, "do not open the on-screen Disconnect window (console only)")
	fake := fs.Bool("fake-camera", false, "offer a synthetic test-pattern camera (testing)")
	child := fs.Bool("child", false, "internal: this is the detached copy")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	rest := fs.Args()

	if len(rest) > 0 {
		switch rest[0] {
		case "version":
			fmt.Println(version.Version)
			return 0
		case "autostart":
			if len(rest) != 2 || (rest[1] != "install" && rest[1] != "uninstall") {
				fmt.Fprintln(os.Stderr, "usage: jarvis-host autostart install|uninstall")
				return 2
			}
			if err := autostart(rest[1] == "install"); err != nil {
				fmt.Fprintln(os.Stderr, "autostart:", err)
				return 1
			}
			return 0
		}
	}

	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot use a config folder:", err)
		return 1
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(rest) > 0 && rest[0] == "forget" {
		cfg.Server, cfg.Token = "", ""
		if err := cfg.Save(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Forgotten: the server address and this computer's pass.")
		return 0
	}

	// the ONE input: the server address (argument > flag > environment > remembered > ask)
	input := *server
	if input == "" && len(rest) > 0 {
		input = rest[0]
	}
	if input == "" {
		input = os.Getenv("JARVIS_SERVER")
	}
	if input == "" {
		input = cfg.Server
	}
	if input == "" {
		if !isTerminal() {
			fmt.Fprintln(os.Stderr, "No server address. Pass it as an argument, --server, or JARVIS_SERVER.")
			return 2
		}
		fmt.Print("Address of your Jarvis server (e.g. https://jarvis.example.com): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(line)
	}
	wsURL, err := config.WebSocketURL(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if cfg.Server != input {
		if cfg.Server != "" { // a different server: the old pass means nothing there
			cfg.Token = ""
		}
		cfg.Server = input
	}
	if *show {
		cfg.ConsoleVisible = true
	}
	if *hidden {
		cfg.ConsoleVisible = false
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "could not save settings:", err)
		return 1
	}

	if *background && !*child {
		pid, err := relaunchDetached(os.Args[1:], dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not start in the background:", err)
			return 1
		}
		fmt.Printf("Jarvis Support Host is running in the background (pid %d). Log: %s\n", pid, dir)
		return 0
	}

	if !cfg.ConsoleVisible {
		ui.SetConsoleVisible(false)
	}
	log := audit.New(dir)
	var ind *ui.Indicator
	if !*noIndicator {
		ind = ui.NewIndicator()
	}
	ui.Banner(fmt.Sprintf("Jarvis Support Host %s", version.Version), "Connecting out to "+wsURL, "Action log: "+log.Path())
	a := agent.New(agent.Options{URL: wsURL, Config: cfg, Audit: log, Code: *code, Name: *name,
		Version: version.Version, Camera: camera.NewManager(*fake), Indicator: ind})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go watchStdin(cancel)
	err = a.Run(ctx)
	switch {
	case errors.Is(err, agent.ErrStopped):
		ui.Banner("Disconnected. Jarvis can no longer reach this computer. Run the app again to reconnect.")
		return 0
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ui.Logf("stopped")
	return 0
}

// watchStdin lets the person type "stop" (or q) in the console to disconnect.
func watchStdin(cancel context.CancelFunc) {
	if !isTerminal() {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		switch strings.ToLower(strings.TrimSpace(sc.Text())) {
		case "stop", "q", "quit", "exit", "disconnect":
			cancel()
			return
		}
	}
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) // (/dev/null is a character device but not a terminal)
}
