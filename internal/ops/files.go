package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jarvis-support/host/internal/protocol"
)

// Upload receives a file from the server: binary frames on the file channel, then an "end" frame.
type Upload struct {
	mu   sync.Mutex
	f    *os.File
	h    hash.Hash
	n    int64
	Done chan struct{}
	once sync.Once
	err  error
}

// Write appends a received chunk.
func (u *Upload) Write(p []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return
	}
	if _, err := u.f.Write(p); err != nil {
		u.err = err
		return
	}
	u.h.Write(p)
	u.n += int64(len(p))
}

// End marks the transfer complete.
func (u *Upload) End() { u.once.Do(func() { close(u.Done) }) }

// PutArgs describes an upload.
type PutArgs struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Overwrite bool   `json:"overwrite"`
}

// BeginPut prepares to receive a file; the returned Upload must be fed by the connection loop
// until End, then FinishPut verifies and moves it into place.
func BeginPut(raw json.RawMessage) (*Upload, *PutArgs, error) {
	var a PutArgs
	if err := decode(raw, &a); err != nil {
		return nil, nil, err
	}
	if a.Path == "" {
		return nil, nil, errors.New("no path given")
	}
	a.Path = expandHome(a.Path)
	if _, err := os.Stat(a.Path); err == nil && !a.Overwrite {
		return nil, nil, errors.New("file exists: " + a.Path)
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(a.Path), ".jarvis-upload-")
	if err != nil {
		return nil, nil, err
	}
	return &Upload{f: f, h: sha256.New(), Done: make(chan struct{})}, &a, nil
}

// FinishPut waits for the end of the transfer, checks size and SHA-256, and renames into place.
func FinishPut(ctx context.Context, u *Upload, a *PutArgs) (Result, error) {
	defer func() { u.f.Close(); os.Remove(u.f.Name()) }()
	select {
	case <-ctx.Done():
		return nil, errors.New("cancelled")
	case <-u.Done:
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return nil, u.err
	}
	sum := hex.EncodeToString(u.h.Sum(nil))
	if (a.Size > 0 && u.n != a.Size) || (a.SHA256 != "" && sum != a.SHA256) {
		return nil, fmt.Errorf("the file arrived damaged (got %d bytes, sha256 %s)", u.n, sum)
	}
	if err := u.f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(u.f.Name(), a.Path); err != nil {
		return nil, err
	}
	_ = os.Chmod(a.Path, 0o644)
	return Result{"path": a.Path, "size": u.n, "sha256": sum}, nil
}

// Get sends a file to the server in chunks on the file channel (tagged with the request id).
func Get(ctx context.Context, s Sender, id string, raw json.RawMessage) (Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	f, err := os.Open(expandHome(a.Path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return nil, errors.New("not a file: " + a.Path)
	}
	h := sha256.New()
	var total int64
	buf := make([]byte, protocol.ChunkSize)
	for {
		if ctx.Err() != nil {
			return nil, errors.New("cancelled")
		}
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			total += int64(n)
			if e := s.Bytes(protocol.ChFile, id, append([]byte(nil), buf[:n]...)); e != nil {
				return nil, e
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return Result{"size": total, "sha256": hex.EncodeToString(h.Sum(nil))}, nil
}

// List describes a folder.
func List(_ context.Context, _ Sender, _ string, raw json.RawMessage) (Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Path == "" {
		a.Path = "~"
	}
	dir := expandHome(a.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(dir)
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row := map[string]any{"name": e.Name(), "dir": e.IsDir()}
		if info, err := e.Info(); err == nil {
			row["size"] = info.Size()
			row["modified"] = info.ModTime().Format(time.RFC3339)
			row["mode"] = info.Mode().String()
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i]["dir"] != rows[j]["dir"] {
			return rows[i]["dir"].(bool)
		}
		return rows[i]["name"].(string) < rows[j]["name"].(string)
	})
	return Result{"path": abs, "entries": rows}, nil
}
