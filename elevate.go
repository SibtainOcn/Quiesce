// Quiesce - Windows system cleaner and RAM optimizer.
// Copyright (C) 2026 SibtainOcn <https://github.com/SibtainOcn/Quiesce>
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.
//
// You should have received a copy of the GNU General Public License along
// with this program. If not, see <https://www.gnu.org/licenses/>.

package main

// Direct commands run from a normal terminal: after the UAC prompt the
// elevated copy starts hidden, attaches to the caller's console and draws
// there, while the caller waits for it. If attaching fails it opens its own
// window instead, so a run is never invisible.

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// attachFlag is internal: it tells the elevated copy which console to join.
const attachFlag = "--attach"

var (
	modshell32Elev      = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = modshell32Elev.NewProc("ShellExecuteExW")
	procFreeConsole     = modkernel32Title.NewProc("FreeConsole")
	procAttachConsole   = modkernel32Title.NewProc("AttachConsole")
	procAllocConsole    = modkernel32Title.NewProc("AllocConsole")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	swHide                = 0
)

// shellExecuteInfo mirrors SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// splitAttachArgs pulls "--attach <pid>" off the front of args.
func splitAttachArgs(args []string) (pid uint32, rest []string, found bool, err error) {
	if len(args) == 0 || !strings.EqualFold(args[0], attachFlag) {
		return 0, args, false, nil
	}
	if len(args) < 3 {
		return 0, nil, true, errors.New("missing process id or command")
	}
	n, perr := strconv.ParseUint(args[1], 10, 32)
	if perr != nil || n == 0 {
		return 0, nil, true, fmt.Errorf("bad process id %q", args[1])
	}
	return uint32(n), args[2:], true, nil
}

// attachToCaller swaps this process's hidden console for the caller's.
// It reports false when it had to open a new window instead.
func attachToCaller(pid uint32) bool {
	procFreeConsole.Call()
	attached := false
	if r, _, _ := procAttachConsole.Call(uintptr(pid)); r != 0 {
		attached = true
	} else {
		procAllocConsole.Call()
	}
	rebindStdHandles()
	return attached
}

// rebindStdHandles points stdin/stdout/stderr at the current console. The
// old handles belonged to the console that was just freed.
func rebindStdHandles() {
	open := func(name string) (windows.Handle, bool) {
		p, _ := windows.UTF16PtrFromString(name)
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		return h, err == nil
	}
	if h, ok := open("CONOUT$"); ok {
		_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h)
		_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, h)
		os.Stdout = os.NewFile(uintptr(h), "CONOUT$")
		os.Stderr = os.Stdout
	}
	if h, ok := open("CONIN$"); ok {
		_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, h)
		os.Stdin = os.NewFile(uintptr(h), "CONIN$")
	}
}

// RunElevatedInTerminal starts the elevated copy for cmds, waits for it and
// returns its exit code. cmds must already be validated command names.
func RunElevatedInTerminal(cmds []string) int {
	fmt.Println(T("priv.requesting"))

	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("%s\n", Tf("priv.exe_error", err))
		return 1
	}
	params := attachFlag + " " + strconv.Itoa(os.Getpid()) + " " + strings.Join(cmds, " ")

	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exePath)
	par, _ := windows.UTF16PtrFromString(params)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exePath))
	hwnd, _, _ := procGetConsoleWindow.Call()

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		hwnd:         hwnd,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: par,
		lpDirectory:  dir,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	// While the elevated copy owns the screen, Ctrl+C is its job (a safe
	// abort). This process just keeps waiting so the shell stays blocked.
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
		}
	}()

	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			fmt.Printf("  %s%s%s\n", YELLOW, T("cli.uac_declined"), RST)
		} else {
			var errno syscall.Errno
			errors.As(callErr, &errno)
			fmt.Printf("%s\n", Tf("priv.failed", int(errno)))
		}
		return 1
	}
	if info.hProcess == 0 {
		return 0
	}
	defer windows.CloseHandle(info.hProcess)

	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return 1
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 1
	}
	return int(code)
}
