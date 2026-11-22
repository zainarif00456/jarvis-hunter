package audit

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogRecordsAndRedactsSecrets(t *testing.T) {
	l := New(t.TempDir())
	l.Write("camera.start", "rtsp://admin:hunter2@192.168.1.5/stream", errors.New("open http://u:pw@h failed"), time.Second)
	raw, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "hunter2") || strings.Contains(s, "pw@") {
		t.Fatalf("secret leaked: %s", s)
	}
	if !strings.Contains(s, "camera.start") || !strings.Contains(s, "admin:***@") {
		t.Fatalf("entry incomplete: %s", s)
	}
}

func TestLogRotates(t *testing.T) {
	l := New(t.TempDir())
	os.WriteFile(l.Path(), make([]byte, maxSize+1), 0o600)
	l.Write("x", "y", nil, 0)
	if _, err := os.Stat(l.Path() + ".1"); err != nil {
		t.Fatal("expected rotation")
	}
}

func TestNilLogIsSafe(t *testing.T) { var l *Log; l.Write("a", "b", nil, 0) }
