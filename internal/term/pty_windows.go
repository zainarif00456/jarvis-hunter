//go:build windows

package term

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type conPTY struct {
	hpc       windows.Handle
	in, out   *os.File // in: write side to the console; out: read side from it
	process   windows.Handle
	waitOnce  sync.Once
	closeOnce sync.Once
	code      int
}

// Open starts PowerShell (or cmd) on a ConPTY.
func Open(cwd string, cols, rows int) (PTY, string, error) {
	dir := ExpandCwd(cwd)
	shell := "powershell.exe"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "cmd.exe"
	}
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, "", err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		return nil, "", err
	}
	var hpc windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, inR, outW, 0, &hpc); err != nil {
		return nil, "", err
	}
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, "", err
	}
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
		return nil, "", err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))
	cmdline, err := windows.UTF16PtrFromString(shell)
	if err != nil {
		return nil, "", err
	}
	dirp, _ := windows.UTF16PtrFromString(dir)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT,
		nil, dirp, &si.StartupInfo, &pi); err != nil {
		return nil, "", err
	}
	windows.CloseHandle(pi.Thread)
	return &conPTY{hpc: hpc, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out"), process: pi.Process}, dir, nil
}

func (p *conPTY) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *conPTY) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *conPTY) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}
func (p *conPTY) Wait() int {
	p.waitOnce.Do(func() {
		_, _ = windows.WaitForSingleObject(p.process, windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(p.process, &code)
		p.code = int(code)
	})
	return p.code
}
func (p *conPTY) Close() error {
	var err error
	p.closeOnce.Do(func() {
		_ = windows.TerminateProcess(p.process, 1)
		windows.ClosePseudoConsole(p.hpc)
		p.in.Close()
		p.out.Close()
		err = windows.CloseHandle(p.process)
	})
	return err
}

var _ = syscall.EINVAL
