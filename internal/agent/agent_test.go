package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jarvis-support/host/internal/audit"
	"github.com/jarvis-support/host/internal/camera"
	"github.com/jarvis-support/host/internal/config"
	"github.com/jarvis-support/host/internal/protocol"
)

// fakeServer plays the Jarvis side of the protocol.
type fakeServer struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	hellos   []map[string]any
	conns    chan *websocket.Conn
	behavior func(hello map[string]any) (reply map[string]any) // first answer; nil = ready
	received chan protocol.Incoming
	results  chan map[string]any
	stash    map[string]map[string]any
	bin      chan []byte
	identity chan map[string]any
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{t: t, conns: make(chan *websocket.Conn, 8), received: make(chan protocol.Incoming, 64), results: make(chan map[string]any, 64), stash: map[string]map[string]any{},
		bin: make(chan []byte, 64), identity: make(chan map[string]any, 8)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws/host" }

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 26)
	ctx := context.Background()
	_, data, err := c.Read(ctx)
	if err != nil {
		return
	}
	var hello map[string]any
	json.Unmarshal(data, &hello)
	f.mu.Lock()
	f.hellos = append(f.hellos, hello)
	f.mu.Unlock()
	reply := map[string]any{"t": "ready"}
	if f.behavior != nil {
		if r := f.behavior(hello); r != nil {
			reply = r
		}
	}
	raw, _ := json.Marshal(reply)
	c.Write(ctx, websocket.MessageText, raw)
	if reply["t"] == "pending" {
		time.Sleep(200 * time.Millisecond)
		raw, _ = json.Marshal(map[string]any{"t": "ready", "token": "pass-123"})
		c.Write(ctx, websocket.MessageText, raw)
	} else if reply["t"] == "refused" {
		c.Close(websocket.StatusPolicyViolation, "refused")
		return
	}
	f.conns <- c
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			f.bin <- data
			continue
		}
		var in protocol.Incoming
		json.Unmarshal(data, &in)
		if in.T == "res" {
			var m map[string]any
			json.Unmarshal(data, &m)
			f.results <- m
			continue
		}
		if in.T == "identity" {
			var m struct{ Data map[string]any }
			json.Unmarshal(data, &m)
			f.identity <- m.Data
			continue
		}
		f.received <- in
	}
}

func (f *fakeServer) send(c *websocket.Conn, v map[string]any) {
	raw, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, raw); err != nil {
		f.t.Fatal(err)
	}
}

// next returns the next frame of this type (skipping events for other ids).
func (f *fakeServer) next(t string) protocol.Incoming {
	deadline := time.After(10 * time.Second)
	for {
		select {
		case in := <-f.received:
			if in.T == t {
				return in
			}
		case <-deadline:
			f.t.Fatalf("timed out waiting for a %q frame", t)
		}
	}
}

func (f *fakeServer) result(id string) map[string]any {
	if m, ok := f.stash[id]; ok {
		return m
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-f.results:
			if m["id"] == id {
				return m
			}
			f.stash[m["id"].(string)] = m
		case <-deadline:
			f.t.Fatalf("no result for %s", id)
		}
	}
}

func start(t *testing.T, f *fakeServer, mutate func(*Options)) (cancel context.CancelFunc, done chan error, cfg *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg, _ = config.Load(dir)
	o := Options{URL: f.url(), Config: cfg, Audit: audit.New(dir), Version: "test", Camera: camera.NewManager(true)}
	if mutate != nil {
		mutate(&o)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan error, 1)
	a := New(o)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			done <- err
		case <-time.After(5 * time.Second):
		}
		time.Sleep(100 * time.Millisecond) // in-flight requests finish writing their log lines
	})
	return cancel, done, cfg
}

func req(f *fakeServer, c *websocket.Conn, id, op string, args any) {
	raw, _ := json.Marshal(args)
	f.send(c, map[string]any{"t": "req", "id": id, "op": op, "args": json.RawMessage(raw)})
}

func TestConnectsSendsHelloAndIdentitySweep(t *testing.T) {
	f := newFakeServer(t)
	start(t, f, func(o *Options) { o.Name = "brothers-mac" })
	<-f.conns
	ident := <-f.identity
	for _, k := range []string{"user", "hostname", "os", "os_version", "arch", "uptime_s", "addresses", "is_admin"} {
		if _, ok := ident[k]; !ok {
			t.Errorf("sweep lacks %s: %v", k, ident)
		}
	}
	h := f.hellos[0]
	if h["name"] != "brothers-mac" || h["os"] != runtime.GOOS || len(h["host_id"].(string)) != 32 || h["v"] != float64(1) {
		t.Errorf("hello: %v", h)
	}
	if _, has := h["token"]; has {
		t.Error("a first connect has no token")
	}
}

func TestApprovalGivesAPassThatIsSavedAndReused(t *testing.T) {
	f := newFakeServer(t)
	f.behavior = func(h map[string]any) map[string]any {
		if h["token"] == nil {
			return map[string]any{"t": "pending", "message": "wait"}
		}
		return nil
	}
	cancel, done, cfg := start(t, f, nil)
	c := <-f.conns
	<-f.identity // sent once the host has saved its pass
	if cfg.Token != "pass-123" {
		t.Fatalf("pass not saved: %q", cfg.Token)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg.Dir(), "config.json"))
	if !strings.Contains(string(raw), "pass-123") {
		t.Error("pass not persisted")
	}
	c.Close(websocket.StatusGoingAway, "restart") // the server drops it; the host comes back with its pass
	<-f.conns
	if f.hellos[len(f.hellos)-1]["token"] != "pass-123" {
		t.Errorf("reconnect did not use the pass: %v", f.hellos[len(f.hellos)-1])
	}
	cancel()
	<-done
}

func TestRefusalDoesNotCrashAndKeepsTrying(t *testing.T) {
	f := newFakeServer(t)
	f.behavior = func(map[string]any) map[string]any {
		return map[string]any{"t": "refused", "reason": "not_accepting", "message": "closed", "retry_s": 0}
	}
	start(t, f, nil)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.hellos)
		f.mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the host should retry after a refusal")
}

func TestBadTokenIsForgottenSoItAsksAgain(t *testing.T) {
	f := newFakeServer(t)
	n := 0
	f.behavior = func(h map[string]any) map[string]any {
		n++
		if n == 1 {
			return map[string]any{"t": "refused", "reason": "bad_token", "message": "revoked", "retry_s": 0}
		}
		return nil
	}
	_, _, cfg := start(t, f, func(o *Options) { o.Config.Token = "stale" })
	<-f.conns
	if cfg.Token != "" {
		t.Errorf("a refused pass should be dropped, have %q", cfg.Token)
	}
	if f.hellos[1]["token"] != nil {
		t.Error("second attempt should not send the stale pass")
	}
}

func TestInviteCodeIsSentOnlyWithoutAPass(t *testing.T) {
	f := newFakeServer(t)
	start(t, f, func(o *Options) { o.Code = "ABCD-2345" })
	<-f.conns
	if f.hellos[0]["code"] != "ABCD-2345" {
		t.Errorf("hello: %v", f.hellos[0])
	}
}

func TestRunCommandStreamsAndReturnsExitCodeAndIsLogged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	f := newFakeServer(t)
	_, _, cfg := start(t, f, nil)
	c := <-f.conns
	<-f.identity
	req(f, c, "r1", "exec", map[string]any{"command": "echo hi; exit 4", "timeout_s": 10})
	res := f.result("r1")
	if res["ok"] != true || res["data"].(map[string]any)["exit_code"] != float64(4) {
		t.Fatalf("%v", res)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg.Dir(), "actions.log"))
	if !strings.Contains(string(raw), `"action":"exec"`) || !strings.Contains(string(raw), "echo hi; exit 4") {
		t.Errorf("action log: %s", raw)
	}
}

func TestUnknownOperationIsAnErrorNotACrash(t *testing.T) {
	f := newFakeServer(t)
	start(t, f, nil)
	c := <-f.conns
	req(f, c, "r1", "format-the-disk", map[string]any{})
	res := f.result("r1")
	if res["ok"] != false || !strings.Contains(res["error"].(string), "does not know") {
		t.Fatalf("%v", res)
	}
	req(f, c, "r2", "exec", map[string]any{"command": ""})
	if f.result("r2")["ok"] != false {
		t.Fatal("empty command must fail cleanly")
	}
}

func TestCancelStopsALongCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	f := newFakeServer(t)
	start(t, f, nil)
	c := <-f.conns
	req(f, c, "long", "exec", map[string]any{"command": "sleep 60", "timeout_s": 120})
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	f.send(c, map[string]any{"t": "cancel", "id": "long"})
	req(f, c, "after", "exec", map[string]any{"command": "echo ok"})
	f.result("after") // the host is still responsive, and the long one gave up
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancel was slow")
	}
}

func TestFileUploadAndDownloadThroughTheSocket(t *testing.T) {
	f := newFakeServer(t)
	start(t, f, nil)
	c := <-f.conns
	dest := filepath.Join(t.TempDir(), "up.bin")
	data := []byte(strings.Repeat("0123456789", 50_000))
	sum := sha256.Sum256(data)
	req(f, c, "u1", "file.put", map[string]any{"path": dest, "size": len(data), "sha256": hex.EncodeToString(sum[:]), "overwrite": true})
	for i := 0; i < len(data); i += 100_000 {
		c.Write(context.Background(), websocket.MessageBinary, protocol.Pack(protocol.ChFile, "u1", data[i:min(i+100_000, len(data))]))
	}
	f.send(c, map[string]any{"t": "end", "id": "u1"})
	if r := f.result("u1"); r["ok"] != true {
		t.Fatalf("%v", r)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(data) {
		t.Fatal("uploaded file differs")
	}
	req(f, c, "d1", "file.get", map[string]any{"path": dest})
	r := f.result("d1")
	var got []byte
	for len(got) < len(data) {
		select {
		case frame := <-f.bin:
			_, id, p, _ := protocol.Unpack(frame)
			if id != "d1" {
				t.Fatalf("chunk tagged %q", id)
			}
			got = append(got, p...)
		case <-time.After(5 * time.Second):
			t.Fatal("download chunks missing")
		}
	}
	if string(got) != string(data) || r["data"].(map[string]any)["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal("downloaded file differs")
	}
}

func TestTerminalSessionOverTheSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	t.Setenv("SHELL", "/bin/sh")
	f := newFakeServer(t)
	start(t, f, nil)
	c := <-f.conns
	req(f, c, "p1", "pty.open", map[string]any{"id": "term1", "cols": 90, "rows": 25, "cwd": t.TempDir()})
	if f.result("p1")["ok"] != true {
		t.Fatal("pty.open failed")
	}
	c.Write(context.Background(), websocket.MessageBinary, protocol.Pack(protocol.ChPTY, "term1", []byte("echo from-$((1+1))\nexit 3\n")))
	var out strings.Builder
	deadline := time.After(8 * time.Second)
	for !strings.Contains(out.String(), "from-2") {
		select {
		case frame := <-f.bin:
			ch, id, p, _ := protocol.Unpack(frame)
			if ch == protocol.ChPTY && id == "term1" {
				out.Write(p)
			}
		case <-deadline:
			t.Fatalf("terminal output: %q", out.String())
		}
	}
	exit := f.next("evt")
	for exit.ID != "term1" {
		exit = f.next("evt")
	}
}

func TestFakeCameraListsAndStreamsFragmentedMP4(t *testing.T) {
	if _, err := camera.FFmpeg(); err != nil {
		t.Skip("ffmpeg not available")
	}
	f := newFakeServer(t)
	start(t, f, nil)
	c := <-f.conns
	req(f, c, "c1", "camera.list", map[string]any{})
	list := f.result("c1")["data"].(map[string]any)["cameras"].([]any)
	if len(list) == 0 || list[0].(map[string]any)["id"] != "fake" {
		t.Fatalf("cameras: %v", list)
	}
	req(f, c, "c2", "camera.start", map[string]any{"session": "s1", "device": "fake", "width": 320, "bitrate_k": 500})
	if f.result("c2")["ok"] != true {
		t.Fatal("camera.start failed")
	}
	var video []byte
	deadline := time.After(15 * time.Second)
	for len(video) < 20_000 {
		select {
		case frame := <-f.bin:
			if ch, id, p, _ := protocol.Unpack(frame); ch == protocol.ChVideo && id == "s1" {
				video = append(video, p...)
			}
		case <-deadline:
			t.Fatalf("got only %d video bytes", len(video))
		}
	}
	if !strings.Contains(string(video[:64]), "ftyp") || !strings.Contains(string(video), "moov") {
		t.Error("stream does not start with an mp4 init segment")
	}
	req(f, c, "c3", "camera.snap", map[string]any{"device": "fake"})
	f.result("c3")
	req(f, c, "c4", "camera.stop", map[string]any{"session": "s1"})
	f.result("c4")
}

func TestDisconnectRequestStopsForGood(t *testing.T) {
	f := newFakeServer(t)
	_, done, _ := start(t, f, nil)
	c := <-f.conns
	req(f, c, "x", "disconnect", map[string]any{})
	select {
	case err := <-done:
		if err != ErrStopped {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host should stop reconnecting after a requested disconnect")
	}
}

func TestSensitiveInputsAreNotWrittenToTheActionLog(t *testing.T) {
	f := newFakeServer(t)
	_, _, cfg := start(t, f, nil)
	c := <-f.conns
	req(f, c, "c1", "camera.start", map[string]any{"session": "s9", "relay": map[string]any{"host": "10.9.9.9", "username": "admin", "password": "topsecret99"}})
	f.result("c1")
	raw, _ := os.ReadFile(filepath.Join(cfg.Dir(), "actions.log"))
	if strings.Contains(string(raw), "topsecret99") {
		t.Fatalf("camera password leaked into the log: %s", raw)
	}
}
