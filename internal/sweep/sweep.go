// Package sweep reports who and what this machine is (the first thing sent on connect).
package sweep

import (
	"context"
	"net"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// Identity is the initial sweep.
func Identity(consoleVisible bool, agentVersion string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out := map[string]any{
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"agent_version":   agentVersion,
		"pid":             os.Getpid(),
		"console_visible": consoleVisible,
		"is_admin":        IsAdmin(),
		"can_elevate":     CanElevate(),
		"addresses":       Addresses(),
	}
	if u, err := user.Current(); err == nil {
		out["user"] = u.Username
		out["home"] = u.HomeDir
		if u.Name != "" {
			out["full_name"] = u.Name
		}
	}
	if h, err := os.Hostname(); err == nil {
		out["hostname"] = h
	}
	if info, err := host.InfoWithContext(ctx); err == nil {
		out["os_name"] = info.Platform
		out["os_version"] = strings.TrimSpace(info.PlatformVersion)
		out["kernel"] = info.KernelVersion
		out["uptime_s"] = info.Uptime
		out["boot_time"] = info.BootTime
		if info.Hostname != "" {
			out["hostname"] = info.Hostname
		}
	}
	if _, ok := out["uptime_s"]; !ok {
		out["uptime_s"] = 0
	}
	if v, _ := out["os_version"].(string); v == "" {
		out["os_version"] = runtime.GOOS
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		out["shell"] = sh
	}
	return out
}

// System is the fuller picture for host.info: identity plus CPU, memory and disks.
func System(consoleVisible bool, agentVersion string) map[string]any {
	out := Identity(consoleVisible, agentVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		out["cpu_threads"] = n
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out["cpu_model"] = infos[0].ModelName
	}
	if pct, err := cpu.PercentWithContext(ctx, 300*time.Millisecond, false); err == nil && len(pct) > 0 {
		out["cpu_percent"] = round1(pct[0])
	}
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		out["mem_total_mb"] = m.Total >> 20
		out["mem_available_mb"] = m.Available >> 20
	}
	var disks []map[string]any
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, p := range parts {
			if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil && u.Total > 0 {
				disks = append(disks, map[string]any{"mount": p.Mountpoint, "fs": p.Fstype,
					"total_gb": u.Total >> 30, "free_gb": u.Free >> 30})
			}
		}
	}
	out["disks"] = disks
	return out
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// Addresses lists non-loopback unicast addresses, "iface: ip".
func Addresses() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ifc.Name+": "+ipn.IP.String())
		}
	}
	sort.Strings(out)
	return out
}
