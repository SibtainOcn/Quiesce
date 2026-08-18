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
	verbPtr, _ := windows.UTF16PtrFromString("runas")
	filePtr, _ := windows.UTF16PtrFromString(exePath)
	dirPtr, _ := windows.UTF16PtrFromString(dir)

	modshell32 := windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW := modshell32.NewProc("ShellExecuteW")

	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		0,
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(5), // SW_SHOW
	)

	if ret <= 32 {
		fmt.Printf("%s\n", Tf("priv.failed", ret))
	}
	RestoreConsoleMode()
	os.Exit(0)
}
