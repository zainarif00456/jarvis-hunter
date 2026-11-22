// Package agent is the connection to the Jarvis server: dial out over WSS, say hello, report who
// this machine is, then do what Jarvis asks. It reconnects by itself with backoff and never
// listens on a port.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/jarvis-support/host/internal/audit"
	"github.com/jarvis-support/host/internal/camera"
	"github.com/jarvis-support/host/internal/config"
	"github.com/jarvis-support/host/internal/protocol"
	"github.com/jarvis-support/host/internal/sweep"
	"github.com/jarvis-support/host/internal/ui"
)

// ErrStopped is returned by Run when the person at this computer (or Jarvis, on request) ended the
// session on purpose: the app must not reconnect.
var ErrStopped = errors.New("disconnected on purpose")

// Options configure an Agent.
type Options struct {
	URL       string // ws(s)://.../ws/host
	Config    *config.Config
	Audit     *audit.Log
	Code      string // optional invite code
	Name      string // shown to Jarvis; default: hostname
	Version   string
	Camera    *camera.Manager
	Indicator *ui.Indicator // nil: no on-screen window (tests)
	// OnReady is called after each successful connect (tests, status line).
	OnReady func()
}

// Agent is one host app instance.
type Agent struct {
	o Options

	mu      sync.Mutex
	stopped bool
}

// New makes an Agent.
func New(o Options) *Agent {
	if o.Camera == nil {
		o.Camera = camera.NewManager(false)
	}
	return &Agent{o: o}
}

// Run connects and stays connected until ctx ends (returns nil) or the session is ended on purpose
// (returns ErrStopped).
func (a *Agent) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		retry, err := a.once(ctx)
		if errors.Is(err, ErrStopped) {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		wait := backoff
		if retry > wait {
			wait = retry
		}
		if err != nil {
			ui.Logf("not connected: %v (retrying in %s)", err, wait.Round(time.Second))
		}
		wait += time.Duration(rand.Int64N(int64(wait/4) + 1)) // jitter: many hosts do not stampede
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if time.Since(started) > 30*time.Second {
			backoff = time.Second // it was a good connection that dropped: try again soon
		} else if backoff < 60*time.Second {
			backoff *= 2
		}
	}
	return nil
}

func (a *Agent) name() string {
	if a.o.Name != "" {
		return a.o.Name
	}
	if a.o.Config.Name != "" {
		return a.o.Config.Name
	}
	id := sweep.Identity(true, a.o.Version)
	if h, ok := id["hostname"].(string); ok {
		return h
	}
	return "host"
}

// once makes one connection attempt and serves it until it ends. retry is a minimum wait the
// server asked for.
func (a *Agent) once(ctx context.Context) (retry time.Duration, err error) {
	dctx, cancelDial := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dctx, a.o.URL, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": []string{"jarvis-support-host/" + a.o.Version + " (" + runtime.GOOS + ")"}},
	})
	cancelDial()
	if err != nil {
		return 0, err
	}
	conn.SetReadLimit(64 << 20)
	defer conn.CloseNow()

	hello := map[string]any{"t": "hello", "v": protocol.Version, "host_id": a.o.Config.HostID,
		"name": a.name(), "os": runtime.GOOS, "arch": runtime.GOARCH, "agent_version": a.o.Version}
	if a.o.Config.Token != "" {
		hello["token"] = a.o.Config.Token
	} else if a.o.Code != "" {
		hello["code"] = a.o.Code
	}
	raw, _ := json.Marshal(hello)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		return 0, err
	}

	// until "ready": the server may say pending (waiting for the owner), or refuse
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return 0, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var in protocol.Incoming
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.T {
		case "pending":
			ui.Banner("Waiting for the owner of Jarvis to approve this computer...",
				"(Ask them to approve \""+a.name()+"\". Nothing can happen on this computer until they do.)")
			a.o.Audit.Write("enrolment", "waiting for approval", nil, 0)
		case "refused":
			a.o.Audit.Write("refused", in.Reason, nil, 0)
			if in.Reason == "bad_token" {
				a.o.Config.Token = "" // the pass is no longer valid: ask for approval again next time
				_ = a.o.Config.Save()
			}
			return time.Duration(in.RetryS) * time.Second, fmt.Errorf("the server said no: %s", in.Message)
		case "ready":
			if in.Token != "" {
				a.o.Config.Token = in.Token
				if err := a.o.Config.Save(); err != nil {
					ui.Logf("could not save the pass: %v", err)
				}
			}
			return a.serve(ctx, conn)
		}
	}
}

func (a *Agent) serve(ctx context.Context, conn *websocket.Conn) (time.Duration, error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := newSession(sctx, conn, a)
	defer s.shutdown()

	ident := sweep.Identity(ui.Visible(), a.o.Version)
	if err := s.writeJSON(map[string]any{"t": "identity", "data": ident}); err != nil {
		return 0, err
	}
	ui.Banner("CONNECTED: Jarvis can control this computer ("+fmt.Sprint(ident["user"])+"@"+fmt.Sprint(ident["hostname"])+")",
		"Everything Jarvis does is written to: "+a.o.Audit.Path(),
		"To disconnect: press Ctrl+C here, type stop + Enter, or use the Disconnect window.")
	a.o.Audit.Write("connected", a.o.URL, nil, 0)
	if a.o.Indicator != nil {
		a.o.Indicator.Show(fmt.Sprintf("Signed in as %v on %v", ident["user"], ident["hostname"]))
		defer a.o.Indicator.Hide()
	}
	if a.o.OnReady != nil {
		a.o.OnReady()
	}

	errc := make(chan error, 1)
	go func() { errc <- s.readLoop() }()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	var pressed <-chan struct{}
	if a.o.Indicator != nil {
		pressed = a.o.Indicator.Pressed()
	}
	for {
		select {
		case <-ctx.Done():
			a.o.Audit.Write("disconnected", "app closing", nil, 0)
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
			return 0, nil
		case <-pressed:
			a.o.Audit.Write("disconnected", "the person at this computer pressed Disconnect", nil, 0)
			_ = conn.Close(websocket.StatusNormalClosure, "stopped by the user")
			return 0, ErrStopped
		case <-s.stopForever:
			a.o.Audit.Write("disconnected", "Jarvis was asked to disconnect", nil, 0)
			time.Sleep(200 * time.Millisecond) // let the answer leave first
			_ = conn.Close(websocket.StatusNormalClosure, "disconnect requested")
			return 0, ErrStopped
		case <-ping.C:
			if err := s.writeJSON(map[string]any{"t": "ping"}); err != nil {
				return 0, err
			}
		case err := <-errc:
			a.o.Audit.Write("disconnected", "connection lost", err, 0)
			ui.Logf("connection lost: %v", err)
			return 0, err
		}
	}
}
