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

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func IsAdmin() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	var elevation struct {
		TokenIsElevated uint32
	}
	var returnedLen uint32

	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&returnedLen,
	)
	if err != nil {
		return false
	}

	return elevation.TokenIsElevated != 0
}

func EnsureAdmin() {
	if IsAdmin() {
		return
	}

	fmt.Println(T("priv.requesting"))
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("%s\n", Tf("priv.exe_error", err))
		RestoreConsoleMode()
		os.Exit(1)
	}

	dir := filepath.Dir(exePath)

	// In Windows Terminal, relaunch through wt.exe so the admin window opens
	// maximized: Windows Terminal ignores requests to resize an existing
	// window, and its default size is too small for the live screen. If
	// that fails for any reason, fall back to launching qc.exe directly.
	ret := uintptr(0)
	if wt := windowsTerminalPath(); wt != "" {
		ret = shellRunAs(wt, `-w new --maximized "`+exePath+`"`, dir)
	}
	if ret <= 32 {
		ret = shellRunAs(exePath, "", dir)
	}

	if ret <= 32 {
		fmt.Printf("%s\n", Tf("priv.failed", ret))
	}
	RestoreConsoleMode()
	os.Exit(0)
}

// windowsTerminalPath returns wt.exe's path when Quiesce is running inside
// Windows Terminal and wt.exe can be found, else "".
func windowsTerminalPath() string {
	if os.Getenv("WT_SESSION") == "" && consoleWindowClass() != "PseudoConsoleWindow" {
		return ""
	}
	p, err := exec.LookPath("wt.exe")
	if err != nil {
		return ""
	}
	return p
}

// shellRunAs starts file elevated (UAC prompt) and returns ShellExecute's
// result: a value of 32 or less means it failed.
func shellRunAs(file, params, dir string) uintptr {
	verbPtr, _ := windows.UTF16PtrFromString("runas")
	filePtr, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0
	}
	dirPtr, _ := windows.UTF16PtrFromString(dir)
	var paramsPtr *uint16
	if params != "" {
		if paramsPtr, err = windows.UTF16PtrFromString(params); err != nil {
			return 0
		}
	}

	modshell32 := windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW := modshell32.NewProc("ShellExecuteW")
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(5), // SW_SHOW
	)
	return ret
}
