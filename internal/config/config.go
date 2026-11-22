// Package config keeps the few things the host remembers between runs: which server, who it is,
// and the pass the server gave it. Stored in the user's config folder with mode 0600.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is what is saved.
type Config struct {
	Server         string `json:"server"`
	HostID         string `json:"host_id"`
	Token          string `json:"token,omitempty"`
	Name           string `json:"name,omitempty"`
	ConsoleVisible bool   `json:"console_visible"`

	dir string
}

// Dir is the config folder (override with JARVIS_HOST_HOME, e.g. for a USB stick or tests).
func Dir() (string, error) {
	if d := os.Getenv("JARVIS_HOST_HOME"); d != "" {
		return d, os.MkdirAll(d, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "jarvis-support-host")
	return d, os.MkdirAll(d, 0o700)
}

// Load reads the saved config (or starts a new one with a fresh host id).
func Load(dir string) (*Config, error) {
	c := &Config{ConsoleVisible: true, dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("config.json is damaged: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if c.HostID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		c.HostID = hex.EncodeToString(b)
	}
	return c, nil
}

// Dir returns the folder the config lives in.
func (c *Config) Dir() string { return c.dir }

// Save writes the config atomically with private permissions.
func (c *Config) Save() error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".config-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !isWindows() {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(c.dir, "config.json"))
}

func isWindows() bool { return os.PathSeparator == '\\' }

// WebSocketURL turns what a person types ("jarvis.example.com", "https://x", "10.0.0.5:8000",
// "http://localhost:8000/") into the ws(s) URL of the host door.
func WebSocketURL(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", errors.New("the server address is empty")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a server address", input)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported scheme %q (use https or http)", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/ws/host") {
		u.Path += "/ws/host"
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}
