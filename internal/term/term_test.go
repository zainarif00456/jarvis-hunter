//go:build !windows

package term

import (
	"strings"
	"testing"
	"time"
)

func TestPTYRunsAShellAndEchoesInput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p, dir, err := Open(t.TempDir(), 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if dir == "" {
		t.Error("no working directory reported")
	}
	if _, err := p.Write([]byte("echo jarvis-$((20+22))\n")); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(got.String(), "jarvis-42") {
		n, _ := p.Read(buf)
		got.Write(buf[:n])
	}
	if !strings.Contains(got.String(), "jarvis-42") {
		t.Fatalf("output was %q", got.String())
	}
	if err := p.Resize(120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
}

func TestPTYExitCode(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p, _, err := Open("", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	p.Write([]byte("exit 5\n"))
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := p.Read(buf); err != nil {
				return
			}
		}
	}()
	if code := p.Wait(); code != 5 {
		t.Fatalf("exit code %d", code)
	}
	p.Close()
	p.Close() // twice is fine
}

func TestExpandCwdFallsBackToHome(t *testing.T) {
	if ExpandCwd("/definitely/not/here") != ExpandCwd("") {
		t.Error("a missing folder should fall back to home")
	}
}
