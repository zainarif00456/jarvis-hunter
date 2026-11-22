// Package camera is the host's camera command set: list cameras (built-in webcam first), capture a
// still, and stream live fragmented MP4 to the server. It drives ffmpeg (a separate program: Go
// cannot open webcams without per-OS native code), found next to this app or on PATH. A network
// camera (RTSP/HTTP) can be relayed with a login that lives in memory only.
package camera

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jarvis-support/host/internal/ops"
	"github.com/jarvis-support/host/internal/protocol"
)

// Manager owns the running camera streams.
type Manager struct {
	// Fake makes a synthetic "Test Pattern" camera appear (for tests and demos without hardware).
	Fake bool

	mu       sync.Mutex
	sessions map[string]context.CancelFunc
}

// NewManager makes a Manager.
func NewManager(fake bool) *Manager {
	return &Manager{Fake: fake || os.Getenv("JARVIS_HOST_FAKE_CAMERA") == "1", sessions: map[string]context.CancelFunc{}}
}

// FFmpeg finds ffmpeg: $JARVIS_HOST_FFMPEG, next to this app (./ffmpeg, ./bin/ffmpeg), or on PATH.
func FFmpeg() (string, error) {
	if p := os.Getenv("JARVIS_HOST_FFMPEG"); p != "" {
		return p, nil
	}
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	if exe, err := os.Executable(); err == nil {
		for _, c := range []string{filepath.Join(filepath.Dir(exe), name), filepath.Join(filepath.Dir(exe), "bin", name)} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
		}
	}
	return exec.LookPath(name)
}

// Device is one camera.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // builtin | usb | fake
}

var builtinWords = regexp.MustCompile(`(?i)facetime|built-?in|integrated|internal|isight|webcam hd|laptop`)

func markKinds(ds []Device) []Device {
	for i := range ds {
		if ds[i].Kind == "" {
			ds[i].Kind = "usb"
			if builtinWords.MatchString(ds[i].Name) {
				ds[i].Kind = "builtin"
			}
		}
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].Kind == "builtin" && ds[j].Kind != "builtin" })
	return ds
}

var (
	avfLine  = regexp.MustCompile(`\[(\d+)\] (.+)$`)
	dshowCam = regexp.MustCompile(`"([^"]+)" \(video\)`)
)

// ParseAVFoundation reads `ffmpeg -f avfoundation -list_devices true -i ""` output.
func ParseAVFoundation(text string) []Device {
	var out []Device
	in := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.Contains(line, "AVFoundation video devices"):
			in = true
		case strings.Contains(line, "AVFoundation audio devices"):
			in = false
		case in:
			if m := avfLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !strings.Contains(strings.ToLower(m[2]), "capture screen") {
				out = append(out, Device{ID: m[1], Name: strings.TrimSpace(m[2])})
			}
		}
	}
	return markKinds(out)
}

// ParseDShow reads `ffmpeg -list_devices true -f dshow -i dummy` output.
func ParseDShow(text string) []Device {
	var out []Device
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if m := dshowCam.FindStringSubmatch(line); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, Device{ID: m[1], Name: m[1]})
		}
	}
	return markKinds(out)
}

func linuxDevices() []Device {
	var out []Device
	nodes, _ := filepath.Glob("/dev/video*")
	for _, n := range nodes {
		base := filepath.Base(n)
		if idx, err := os.ReadFile("/sys/class/video4linux/" + base + "/index"); err == nil && strings.TrimSpace(string(idx)) != "0" {
			continue // a metadata node, not the capture node
		}
		name := base
		if raw, err := os.ReadFile("/sys/class/video4linux/" + base + "/name"); err == nil {
			name = strings.TrimSpace(string(raw))
		}
		out = append(out, Device{ID: n, Name: name})
	}
	return markKinds(out)
}

// List enumerates cameras.
func (m *Manager) List(ctx context.Context, _ ops.Sender, _ string, _ json.RawMessage) (ops.Result, error) {
	var devs []Device
	note := ""
	if m.Fake {
		devs = append(devs, Device{ID: "fake", Name: "Test Pattern", Kind: "fake"})
	}
	ff, ffErr := FFmpeg()
	switch runtime.GOOS {
	case "linux":
		devs = append(devs, linuxDevices()...)
	case "darwin":
		if ffErr == nil {
			out, _ := exec.CommandContext(ctx, ff, "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "").CombinedOutput()
			devs = append(devs, ParseAVFoundation(string(out))...)
		}
	case "windows":
		if ffErr == nil {
			out, _ := exec.CommandContext(ctx, ff, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy").CombinedOutput()
			devs = append(devs, ParseDShow(string(out))...)
		}
	}
	if ffErr != nil && !m.Fake {
		note = "ffmpeg was not found: put it next to this app or on PATH to use cameras"
	}
	if devs == nil {
		devs = []Device{}
	}
	return ops.Result{"cameras": devs, "note": note}, nil
}

type source struct {
	Device string `json:"device"`
	Relay  *struct {
		URL      string `json:"url"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"relay"`
	Session  string `json:"session"`
	Width    int    `json:"width"`
	BitrateK int    `json:"bitrate_k"`
}

// RelayURL builds the stream address of a network camera. The login stays in memory.
func RelayURL(r struct {
	URL      string `json:"url"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	Username string `json:"username"`
	Password string `json:"password"`
}) (string, error) {
	if r.URL != "" {
		u, err := url.Parse(r.URL)
		if err != nil || u.Host == "" {
			return "", errors.New("that is not a camera address")
		}
		if u.User == nil && r.Username != "" {
			u.User = url.UserPassword(r.Username, r.Password)
		}
		return u.String(), nil
	}
	if r.Host == "" {
		return "", errors.New("no camera address given")
	}
	host := r.Host
	if r.Port != 0 {
		host = fmt.Sprintf("%s:%d", r.Host, r.Port)
	}
	u := url.URL{Scheme: "rtsp", Host: host, Path: r.Path}
	if u.Path == "" {
		u.Path = "/"
	}
	if r.Username != "" {
		u.User = url.UserPassword(r.Username, r.Password)
	}
	return u.String(), nil
}

// InputArgs are the ffmpeg input options for a source.
func InputArgs(s source) ([]string, error) {
	if s.Relay != nil {
		addr, err := RelayURL(*s.Relay)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(addr, "rtsp") {
			return []string{"-rtsp_transport", "tcp", "-timeout", "8000000", "-fflags", "nobuffer", "-i", addr}, nil
		}
		return []string{"-rw_timeout", "8000000", "-reconnect", "1", "-i", addr}, nil
	}
	switch {
	case s.Device == "fake":
		return []string{"-re", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=15"}, nil
	case s.Device == "":
		return nil, errors.New("no camera chosen")
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{"-f", "avfoundation", "-framerate", "30", "-video_size", "1280x720", "-pix_fmt", "uyvy422", "-i", s.Device + ":none"}, nil
	case "windows":
		return []string{"-f", "dshow", "-i", "video=" + s.Device}, nil
	default:
		return []string{"-f", "v4l2", "-framerate", "20", "-video_size", "1280x720", "-i", s.Device}, nil
	}
}

func decodeSource(raw json.RawMessage) (source, error) {
	var s source
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("bad arguments: %w", err)
	}
	return s, nil
}

// Snap captures one JPEG and sends it on the file channel.
func (m *Manager) Snap(ctx context.Context, snd ops.Sender, id string, raw json.RawMessage) (ops.Result, error) {
	ff, err := FFmpeg()
	if err != nil {
		return nil, errors.New("ffmpeg was not found: cameras need it")
	}
	s, err := decodeSource(raw)
	if err != nil {
		return nil, err
	}
	in, err := InputArgs(s)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, in...)
	args = append(args, "-frames:v", "1", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "2", "pipe:1")
	var stderr bytes.Buffer
	cmd := exec.CommandContext(cctx, ff, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil, fmt.Errorf("no picture: %s", tail(stderr.String()))
	}
	for i := 0; i < len(out); i += protocol.ChunkSize {
		if err := snd.Bytes(protocol.ChFile, id, out[i:min(i+protocol.ChunkSize, len(out))]); err != nil {
			return nil, err
		}
	}
	return ops.Result{"format": "jpeg", "size": len(out)}, nil
}

// Start begins streaming fMP4 (H.264, a keyframe every second) on the video channel.
func (m *Manager) Start(ctx context.Context, snd ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	ff, err := FFmpeg()
	if err != nil {
		return nil, errors.New("ffmpeg was not found: cameras need it")
	}
	s, err := decodeSource(raw)
	if err != nil {
		return nil, err
	}
	if s.Session == "" {
		return nil, errors.New("no session id")
	}
	in, err := InputArgs(s)
	if err != nil {
		return nil, err
	}
	width, kbps := s.Width, s.BitrateK
	if width <= 0 {
		width = 1280
	}
	if kbps <= 0 {
		kbps = 1800
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, in...)
	args = append(args, "-an", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", width),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "15", "-keyint_min", "15",
		"-sc_threshold", "0", "-pix_fmt", "yuv420p", "-b:v", strconv.Itoa(kbps)+"k", "-maxrate", strconv.Itoa(kbps)+"k",
		"-bufsize", strconv.Itoa(kbps*2)+"k", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	runCtx, cancel := context.WithCancel(context.Background()) // outlives this request: the stream runs until stopped
	cmd := exec.CommandContext(runCtx, ff, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	m.mu.Lock()
	m.sessions[s.Session] = cancel
	m.mu.Unlock()
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if e := snd.Bytes(protocol.ChVideo, s.Session, append([]byte(nil), buf[:n]...)); e != nil {
					cancel()
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					stderr.WriteString(err.Error())
				}
				break
			}
		}
		_ = cmd.Wait()
		m.mu.Lock()
		_, still := m.sessions[s.Session]
		delete(m.sessions, s.Session)
		m.mu.Unlock()
		if still && runCtx.Err() == nil {
			snd.Event(s.Session, map[string]any{"kind": "camera.ended", "reason": tail(stderr.String())})
		}
		cancel()
	}()
	return ops.Result{"session": s.Session}, nil
}

// Stop ends one stream.
func (m *Manager) Stop(_ context.Context, _ ops.Sender, _ string, raw json.RawMessage) (ops.Result, error) {
	var a struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(raw, &a)
	m.mu.Lock()
	cancel := m.sessions[a.Session]
	delete(m.sessions, a.Session)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return ops.Result{}, nil
}

// StopAll ends every stream (the connection is gone or the user pressed Stop).
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.sessions {
		cancel()
		delete(m.sessions, id)
	}
}

// Active is how many streams run.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	if s == "" {
		return "the camera stopped"
	}
	return s
}
