package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type rec struct {
	mu     sync.Mutex
	out    map[string]string
	frames [][]byte
}

func (r *rec) Event(_ string, f map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.out == nil {
		r.out = map[string]string{}
	}
	if f["kind"] == "out" {
		r.out[f["stream"].(string)] += f["data"].(string)
	}
}
func (r *rec) Bytes(_ byte, _ string, p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, p)
	return nil
}

func args(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func skipWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell syntax differs on Windows")
	}
}

func TestExecStreamsStdoutStderrAndExitCode(t *testing.T) {
	skipWindows(t)
	r := &rec{}
	res, err := Exec(context.Background(), r, "1", args(map[string]any{"command": "echo hello; echo oops 1>&2; exit 7", "cwd": t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	if res["exit_code"] != 7 || strings.TrimSpace(r.out["stdout"]) != "hello" || strings.TrimSpace(r.out["stderr"]) != "oops" {
		t.Fatalf("res=%v out=%v", res, r.out)
	}
	if res["cwd"] == "" {
		t.Error("working directory not reported")
	}
}

func TestExecHonoursCwdAndRejectsMissingOne(t *testing.T) {
	skipWindows(t)
	dir := t.TempDir()
	r := &rec{}
	if _, err := Exec(context.Background(), r, "1", args(map[string]any{"command": "pwd", "cwd": dir})); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if got := strings.TrimSpace(r.out["stdout"]); got != dir && got != real {
		t.Errorf("pwd = %q, want %q", got, dir)
	}
	if _, err := Exec(context.Background(), r, "2", args(map[string]any{"command": "pwd", "cwd": "/no/such/dir"})); err == nil {
		t.Error("missing cwd should fail")
	}
}

func TestExecCancelKillsTheWholeTree(t *testing.T) {
	skipWindows(t)
	ctx, cancel := context.WithCancel(context.Background())
	marker := filepath.Join(t.TempDir(), "still-running")
	done := make(chan error, 1)
	go func() {
		_, err := Exec(ctx, &rec{}, "1", args(map[string]any{"command": "(sleep 1; touch " + marker + ") & sleep 30", "timeout_s": 60}))
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled command should report cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the command")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a child of the cancelled command kept running")
	}
}

func TestExecTimeout(t *testing.T) {
	skipWindows(t)
	start := time.Now()
	res, err := Exec(context.Background(), &rec{}, "1", args(map[string]any{"command": "sleep 20", "timeout_s": 0.4}))
	if err != nil || res["timed_out"] != true || time.Since(start) > 5*time.Second {
		t.Fatalf("res=%v err=%v after %v", res, err, time.Since(start))
	}
}

func TestExecDoesNotHangWaitingForInput(t *testing.T) {
	skipWindows(t)
	res, err := Exec(context.Background(), &rec{}, "1", args(map[string]any{"command": "cat", "timeout_s": 5}))
	if err != nil || res["timed_out"] == true {
		t.Fatalf("stdin must be closed: res=%v err=%v", res, err)
	}
}

func TestUploadVerifiesChecksumAndMovesIntoPlace(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "sub", "file.bin")
	data := []byte(strings.Repeat("jarvis", 1000))
	sum := sha256.Sum256(data)
	u, a, err := BeginPut(args(map[string]any{"path": dest, "size": len(data), "sha256": hex.EncodeToString(sum[:]), "overwrite": true}))
	if err != nil {
		t.Fatal(err)
	}
	u.Write(data[:100])
	u.Write(data[100:])
	u.End()
	res, err := FinishPut(context.Background(), u, a)
	if err != nil || res["size"] != int64(len(data)) {
		t.Fatalf("%v %v", res, err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(data) {
		t.Fatal("content differs")
	}
}

func TestUploadRejectsDamagedFileAndLeavesNothing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x.bin")
	u, a, _ := BeginPut(args(map[string]any{"path": dest, "size": 3, "sha256": strings.Repeat("0", 64), "overwrite": true}))
	u.Write([]byte("abc"))
	u.End()
	if _, err := FinishPut(context.Background(), u, a); err == nil {
		t.Fatal("expected a checksum failure")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a damaged file must not be put in place")
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 0 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestUploadRefusesToOverwriteWhenAskedNotTo(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x")
	os.WriteFile(dest, []byte("old"), 0o644)
	if _, _, err := BeginPut(args(map[string]any{"path": dest, "overwrite": false})); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestGetSendsChunksAndChecksum(t *testing.T) {
	src := filepath.Join(t.TempDir(), "f")
	data := make([]byte, 600_000)
	for i := range data {
		data[i] = byte(i)
	}
	os.WriteFile(src, data, 0o644)
	r := &rec{}
	res, err := Get(context.Background(), r, "1", args(map[string]any{"path": src}))
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for _, f := range r.frames {
		got = append(got, f...)
	}
	sum := sha256.Sum256(data)
	if len(r.frames) < 3 || string(got) != string(data) || res["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("frames=%d equal=%v res=%v", len(r.frames), string(got) == string(data), res)
	}
}

func TestListSortsFoldersFirst(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "b.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(d, "zdir"), 0o755)
	res, err := List(context.Background(), &rec{}, "1", args(map[string]any{"path": d}))
	if err != nil {
		t.Fatal(err)
	}
	rows := res["entries"].([]map[string]any)
	if len(rows) != 2 || rows[0]["name"] != "zdir" {
		t.Fatalf("%v", rows)
	}
}

func TestProcessesIncludesThisOne(t *testing.T) {
	res, err := Processes(context.Background(), &rec{}, "1", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range res["processes"].([]map[string]any) {
		if p["pid"] == int32(os.Getpid()) {
			found = true
		}
	}
	if !found {
		t.Error("own pid missing from the process list")
	}
}

func TestKillRefusesItself(t *testing.T) {
	if _, err := Kill(context.Background(), &rec{}, "1", args(map[string]any{"pid": os.Getpid()})); err == nil {
		t.Fatal("the app must not kill itself through Kill")
	}
}

func TestKillEndsAProcess(t *testing.T) {
	skipWindows(t)
	cmd := newSleeper(t)
	if _, err := Kill(context.Background(), &rec{}, "1", args(map[string]any{"pid": cmd.Process.Pid, "force": true})); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process still alive")
	}
}
