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
	"time"

	"github.com/jarvis-support/host/internal/protocol"
)

const winShot = `Add-Type -AssemblyName System.Windows.Forms,System.Drawing;` +
	`$b=[System.Windows.Forms.SystemInformation]::VirtualScreen;` +
	`$bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height;` +
	`$g=[System.Drawing.Graphics]::FromImage($bmp);$g.CopyFromScreen($b.Location,[System.Drawing.Point]::Empty,$b.Size);` +
	`$bmp.Save($args[0],[System.Drawing.Imaging.ImageFormat]::Png)`

// Screenshot captures the whole screen and sends the PNG on the file channel.
func Screenshot(ctx context.Context, s Sender, id string, _ json.RawMessage) (Result, error) {
	dir, err := os.MkdirTemp("", "jarvis-shot-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "screen.png")
	var tried []string
	run := func(name string, args ...string) bool {
		p, err := exec.LookPath(name)
		if err != nil {
			return false
		}
		tried = append(tried, name)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_ = exec.CommandContext(cctx, p, args...).Run()
		st, err := os.Stat(out)
		return err == nil && st.Size() > 0
	}
	ok := false
	switch runtime.GOOS {
	case "darwin":
		ok = run("screencapture", "-x", "-t", "png", out)
	case "windows":
		ok = run("powershell", "-NoProfile", "-NonInteractive", "-Command", winShot, out)
	default:
		for _, c := range [][]string{
			{"grim", out}, {"gnome-screenshot", "-f", out}, {"spectacle", "-b", "-n", "-f", "-o", out},
			{"maim", out}, {"scrot", "-o", out}, {"import", "-window", "root", out},
			{"ffmpeg", "-loglevel", "error", "-y", "-f", "x11grab", "-i", os.Getenv("DISPLAY"), "-frames:v", "1", out},
		} {
			if ok = run(c[0], c[1:]...); ok {
				break
			}
		}
	}
	if !ok {
		hint := "no screenshot tool worked"
		if runtime.GOOS == "darwin" {
			hint = "screencapture failed: grant Screen Recording permission to this app in System Settings > Privacy & Security"
		}
		if len(tried) > 0 {
			hint += " (tried " + strings.Join(tried, ", ") + ")"
		}
		return nil, errors.New(hint)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(raw); i += protocol.ChunkSize {
		end := min(i+protocol.ChunkSize, len(raw))
		if err := s.Bytes(protocol.ChFile, id, raw[i:end]); err != nil {
			return nil, err
		}
	}
	return Result{"format": "png", "size": len(raw)}, nil
}

var _ = fmt.Sprint
