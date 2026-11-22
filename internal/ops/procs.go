package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v4/process"
)

// Processes lists running processes.
func Processes(ctx context.Context, _ Sender, _ string, _ json.RawMessage) (Result, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(procs))
	for _, p := range procs {
		row := map[string]any{"pid": p.Pid}
		if n, err := p.NameWithContext(ctx); err == nil {
			row["name"] = n
		}
		if u, err := p.UsernameWithContext(ctx); err == nil {
			row["user"] = u
		}
		if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
			row["rss_mb"] = m.RSS >> 20
		}
		if c, err := p.CmdlineWithContext(ctx); err == nil {
			if len(c) > 300 {
				c = c[:300]
			}
			row["cmd"] = c
		}
		rows = append(rows, row)
	}
	return Result{"processes": rows}, nil
}

// Kill ends a process by pid.
func Kill(_ context.Context, _ Sender, _ string, raw json.RawMessage) (Result, error) {
	var a struct {
		Pid   int32 `json:"pid"`
		Force bool  `json:"force"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Pid <= 0 || int(a.Pid) == os.Getpid() {
		return nil, errors.New("refusing: that is not a valid target (or it is this app)")
	}
	return Result{}, terminate(a.Pid, a.Force)
}

func terminate(pid int32, force bool) error {
	p, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		args := []string{"/PID", fmt.Sprint(pid), "/T"}
		if force {
			args = append(args, "/F")
		}
		out, err := exec.Command("taskkill", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(utf8(out)))
		}
		return nil
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return p.Signal(sig)
}

// Launch opens an application.
func Launch(_ context.Context, _ Sender, _ string, raw json.RawMessage) (Result, error) {
	var a struct {
		App  string   `json:"app"`
		Args []string `json:"args"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.App) == "" {
		return nil, errors.New("no application given")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat(a.App); err == nil || strings.HasSuffix(a.App, ".app") && strings.Contains(a.App, "/") {
			cmd = exec.Command("open", append([]string{a.App}, argsAfter("--args", a.Args)...)...)
		} else {
			cmd = exec.Command("open", append([]string{"-a", a.App}, argsAfter("--args", a.Args)...)...)
		}
	case "windows":
		cmd = exec.Command("cmd.exe")
		line := `cmd.exe /C start "" "` + a.App + `"`
		for _, x := range a.Args {
			line += ` "` + x + `"`
		}
		setCmdLine(cmd, line)
	default:
		path, err := exec.LookPath(a.App)
		if err != nil {
			if abs, e := filepath.Abs(a.App); e == nil {
				if _, e2 := os.Stat(abs); e2 == nil {
					path = abs
				}
			}
		}
		if path == "" {
			if gl, e := exec.LookPath("gtk-launch"); e == nil {
				cmd = exec.Command(gl, a.App)
			} else {
				return nil, fmt.Errorf("%q was not found", a.App)
			}
		} else {
			cmd = exec.Command(path, a.Args...)
		}
		detach(cmd)
	}
	if out, err := cmd.CombinedOutput(); err != nil && runtime.GOOS != "linux" {
		return nil, fmt.Errorf("could not open %s: %v %s", a.App, err, strings.TrimSpace(utf8(out)))
	}
	return Result{"detail": a.App + " opened"}, nil
}

func argsAfter(flag string, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	return append([]string{flag}, args...)
}

// CloseApp ends every process whose name matches (politely, or hard with force).
func CloseApp(ctx context.Context, _ Sender, _ string, raw json.RawMessage) (Result, error) {
	var a struct {
		App   string `json:"app"`
		Force bool   `json:"force"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a.App), ".exe"))
	if want == "" {
		return nil, errors.New("no application given")
	}
	if runtime.GOOS == "darwin" && !a.Force {
		if err := exec.Command("osascript", "-e", `tell application "`+strings.ReplaceAll(a.App, `"`, "")+`" to quit`).Run(); err == nil {
			return Result{"closed": 1}, nil
		}
	}
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	closed := 0
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil || int(p.Pid) == os.Getpid() {
			continue
		}
		low := strings.ToLower(strings.TrimSuffix(name, ".exe"))
		if low == want || (len(want) > 3 && strings.Contains(low, want)) {
			if terminate(p.Pid, a.Force) == nil {
				closed++
			}
		}
	}
	if closed == 0 {
		return nil, fmt.Errorf("no running application matches %q", a.App)
	}
	return Result{"closed": closed}, nil
}
