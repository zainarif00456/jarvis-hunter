package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWebSocketURL(t *testing.T) {
	cases := map[string]string{
		"jarvis.example.com":            "wss://jarvis.example.com/ws/host",
		"https://jarvis.example.com/":   "wss://jarvis.example.com/ws/host",
		"http://localhost:8000":         "ws://localhost:8000/ws/host",
		"10.0.0.5:8000":                 "wss://10.0.0.5:8000/ws/host",
		"wss://x.y/base/":               "wss://x.y/base/ws/host",
		"https://x.y/ws/host":           "wss://x.y/ws/host",
		"  https://x.y?token=secret#a ": "wss://x.y/ws/host",
	}
	for in, want := range cases {
		got, err := WebSocketURL(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "ftp://x", "http://"} {
		if _, err := WebSocketURL(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestSaveLoadKeepsIdentityAndIsPrivate(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil || len(c.HostID) != 32 {
		t.Fatalf("fresh config: %v %q", err, c.HostID)
	}
	c.Server, c.Token = "https://x", "tok"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil || again.HostID != c.HostID || again.Token != "tok" || !again.ConsoleVisible {
		t.Fatalf("reload mismatch: %+v %v", again, err)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(filepath.Join(dir, "config.json"))
		if st.Mode().Perm() != 0o600 {
			t.Errorf("config mode %v, want 0600", st.Mode().Perm())
		}
	}
}

func TestDamagedConfigIsReportedNotIgnored(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{nope"), 0o600)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an error")
	}
}
