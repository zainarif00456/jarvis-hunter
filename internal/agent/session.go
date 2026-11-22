package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/jarvis-support/host/internal/ops"
	"github.com/jarvis-support/host/internal/protocol"
	"github.com/jarvis-support/host/internal/sweep"
	"github.com/jarvis-support/host/internal/term"
	"github.com/jarvis-support/host/internal/ui"
)

type handler func(ctx context.Context, s ops.Sender, id string, args json.RawMessage) (ops.Result, error)

// session is one live connection.
type session struct {
	ctx  context.Context
	conn *websocket.Conn
	a    *Agent

	mu       sync.Mutex
	inflight map[string]context.CancelFunc
	uploads  map[string]*ops.Upload
	ptys     map[string]term.PTY

	stopForever chan struct{}
	stopOnce    sync.Once
	handlers    map[string]handler
}

func newSession(ctx context.Context, conn *websocket.Conn, a *Agent) *session {
	s := &session{ctx: ctx, conn: conn, a: a, inflight: map[string]context.CancelFunc{},
		uploads: map[string]*ops.Upload{}, ptys: map[string]term.PTY{}, stopForever: make(chan struct{})}
	cam := a.o.Camera
	s.handlers = map[string]handler{
		"exec": ops.Exec, "file.get": ops.Get, "fs.list": ops.List, "ps.list": ops.Processes,
		"ps.kill": ops.Kill, "app.launch": ops.Launch, "app.close": ops.CloseApp, "screenshot": ops.Screenshot,
		"camera.list": cam.List, "camera.snap": cam.Snap, "camera.start": cam.Start, "camera.stop": cam.Stop,
		"sysinfo": s.sysinfo, "console.set": s.console, "disconnect": s.disconnect,
		"pty.open": s.ptyOpen, "pty.resize": s.ptyResize, "pty.close": s.ptyClose,
	}
	return s
}

// --- ops.Sender -------------------------------------------------------------------------

func (s *session) writeJSON(v map[string]any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, raw)
}

// Event implements ops.Sender.
func (s *session) Event(id string, fields map[string]any) {
	m := map[string]any{"t": "evt", "id": id}
	for k, v := range fields {
		m[k] = v
	}
	_ = s.writeJSON(m)
}

// Bytes implements ops.Sender.
func (s *session) Bytes(ch byte, id string, payload []byte) error {
	ctx, cancel := context.WithTimeout(s.ctx, 120*time.Second)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageBinary, protocol.Pack(ch, id, payload))
}

// --- reading ------------------------------------------------------------------------------

func (s *session) readLoop() error {
	for {
		typ, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			s.onBinary(data)
			continue
		}
		var in protocol.Incoming
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.T {
		case "req":
			s.startRequest(in)
		case "cancel":
			s.mu.Lock()
			cancel := s.inflight[in.ID]
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case "end":
			s.mu.Lock()
			u := s.uploads[in.ID]
			s.mu.Unlock()
			if u != nil {
				u.End()
			}
		}
	}
}

func (s *session) onBinary(frame []byte) {
	ch, id, payload, err := protocol.Unpack(frame)
	if err != nil {
		return
	}
	switch ch {
	case protocol.ChPTY:
		s.mu.Lock()
		p := s.ptys[id]
		s.mu.Unlock()
		if p != nil {
			_, _ = p.Write(payload)
		}
	case protocol.ChFile:
		s.mu.Lock()
		u := s.uploads[id]
		s.mu.Unlock()
		if u != nil {
			u.Write(payload)
		}
	}
}

// --- requests -----------------------------------------------------------------------------

func (s *session) startRequest(in protocol.Incoming) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.inflight[in.ID] = cancel
	s.mu.Unlock()
	// An upload must be ready before its first chunk (sent right behind the request) is read.
	var upload *ops.Upload
	var putArgs *ops.PutArgs
	var putErr error
	if in.Op == "file.put" {
		var pa *ops.PutArgs
		upload, pa, putErr = ops.BeginPut(in.Args)
		if pa != nil {
			putArgs = pa
		}
		if upload != nil {
			s.mu.Lock()
			s.uploads[in.ID] = upload
			s.mu.Unlock()
		}
	}
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.inflight, in.ID)
			delete(s.uploads, in.ID)
			s.mu.Unlock()
		}()
		start := time.Now()
		var res ops.Result
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			if in.Op == "file.put" {
				if putErr != nil {
					err = putErr
				} else {
					res, err = ops.FinishPut(ctx, upload, putArgs)
				}
				return
			}
			res, err = s.run(ctx, in)
		}()
		s.a.o.Audit.Write(in.Op, summarize(in), err, time.Since(start))
		if ctx.Err() != nil && err != nil {
			return // cancelled by the server: it is no longer waiting for an answer
		}
		if err != nil {
			_ = s.writeJSON(map[string]any{"t": "res", "id": in.ID, "ok": false, "error": err.Error()})
			return
		}
		if res == nil {
			res = ops.Result{}
		}
		_ = s.writeJSON(map[string]any{"t": "res", "id": in.ID, "ok": true, "data": res})
	}()
}

func (s *session) run(ctx context.Context, in protocol.Incoming) (ops.Result, error) {
	h, ok := s.handlers[in.Op]
	if !ok {
		return nil, errors.New("this host does not know the operation " + in.Op)
	}
	return h(ctx, s, in.ID, in.Args)
}

// summarize describes a request for the action log without secrets or file contents.
func summarize(in protocol.Incoming) string {
	var m map[string]any
	_ = json.Unmarshal(in.Args, &m)
	pick := func(keys ...string) string {
		var parts []string
		for _, k := range keys {
			if v, ok := m[k]; ok && v != nil && v != "" {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
		}
		return strings.Join(parts, " ")
	}
	switch in.Op {
	case "exec":
		return pick("command", "cwd", "visible")
	case "file.put":
		return pick("path", "size")
	case "file.get", "fs.list":
		return pick("path")
	case "app.launch", "app.close":
		return pick("app", "force")
	case "ps.kill":
		return pick("pid", "force")
	case "camera.start", "camera.snap":
		if _, relay := m["relay"]; relay {
			return "network camera relay"
		}
		return pick("device")
	case "pty.open":
		return pick("cwd")
	}
	return ""
}

// --- local operations -----------------------------------------------------------------------

func (s *session) sysinfo(context.Context, ops.Sender, string, json.RawMessage) (ops.Result, error) {
	return sweep.System(ui.Visible(), s.a.o.Version), nil
}

func (s *session) console(_ context.Context, _ ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	var a struct {
		Visible bool `json:"visible"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	mode := ui.SetConsoleVisible(a.Visible)
	s.a.o.Config.ConsoleVisible = a.Visible
	_ = s.a.o.Config.Save()
	return ops.Result{"visible": a.Visible, "mode": mode}, nil
}

func (s *session) disconnect(context.Context, ops.Sender, string, json.RawMessage) (ops.Result, error) {
	s.stopOnce.Do(func() { close(s.stopForever) })
	return ops.Result{}, nil
}

func (s *session) shutdown() {
	s.mu.Lock()
	for _, cancel := range s.inflight {
		cancel()
	}
	ptys := s.ptys
	s.ptys = map[string]term.PTY{}
	s.mu.Unlock()
	for _, p := range ptys {
		_ = p.Close()
	}
	s.a.o.Camera.StopAll()
}

// --- terminal -------------------------------------------------------------------------------

func (s *session) ptyOpen(_ context.Context, _ ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	var a struct {
		ID   string `json:"id"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
		Cwd  string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.ID == "" {
		return nil, errors.New("bad arguments")
	}
	if a.Cols <= 0 {
		a.Cols = 100
	}
	if a.Rows <= 0 {
		a.Rows = 30
	}
	p, dir, err := term.Open(a.Cwd, a.Cols, a.Rows)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.ptys[a.ID] = p
	s.mu.Unlock()
	go func() {
		buf := make([]byte, 32<<10)
		var total int
		for {
			n, err := p.Read(buf)
			if n > 0 {
				total += n
				if e := s.Bytes(protocol.ChPTY, a.ID, append([]byte(nil), buf[:n]...)); e != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		code := p.Wait()
		s.mu.Lock()
		delete(s.ptys, a.ID)
		s.mu.Unlock()
		_ = p.Close()
		s.a.o.Audit.Write("pty.closed", fmt.Sprintf("terminal %s ended, %d bytes of output (keystrokes are never logged)", a.ID, total), nil, 0)
		s.Event(a.ID, map[string]any{"kind": "pty.exit", "code": code})
	}()
	return ops.Result{"cwd": dir}, nil
}

func (s *session) ptyResize(_ context.Context, _ ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	var a struct {
		ID   string `json:"id"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	s.mu.Lock()
	p := s.ptys[a.ID]
	s.mu.Unlock()
	if p != nil && a.Cols > 0 && a.Rows > 0 {
		_ = p.Resize(a.Cols, a.Rows)
	}
	return ops.Result{}, nil
}

func (s *session) ptyClose(_ context.Context, _ ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &a)
	s.mu.Lock()
	p := s.ptys[a.ID]
	s.mu.Unlock()
	if p != nil {
		_ = p.Close()
	}
	return ops.Result{}, nil
}
