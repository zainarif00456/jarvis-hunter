// Package audit is the action log on the host: one JSON line for everything Jarvis does here, so
// whoever sits at the machine can see exactly what happened. It records what was asked (with
// passwords and camera logins removed), never file contents or keystrokes.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const maxSize = 5 << 20

// Log appends to <dir>/actions.log, rotating to actions.log.1 at 5 MB.
type Log struct {
	mu   sync.Mutex
	path string
}

// New opens (creating if needed) the log in dir.
func New(dir string) *Log { return &Log{path: filepath.Join(dir, "actions.log")} }

// Path of the log file.
func (l *Log) Path() string { return l.path }

var secretURL = regexp.MustCompile(`(?i)([a-z]+://[^:/\s@]+:)[^@\s]+@`)

// Redact removes passwords from URLs inside a string.
func Redact(s string) string { return secretURL.ReplaceAllString(s, "${1}***@") }

// Write records one event. kind is free text ("exec", "file.put", "connected"...).
func (l *Log) Write(kind, detail string, err error, took time.Duration) {
	if l == nil {
		return
	}
	entry := map[string]any{"ts": time.Now().Format(time.RFC3339), "action": kind, "detail": Redact(detail)}
	if err != nil {
		entry["error"] = Redact(err.Error())
	}
	if took > 0 {
		entry["ms"] = took.Milliseconds()
	}
	line, _ := json.Marshal(entry)
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, e := os.Stat(l.path); e == nil && st.Size() > maxSize {
		os.Rename(l.path, l.path+".1")
	}
	f, e := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if e != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}
