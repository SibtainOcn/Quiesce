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

	fmt.Println("Requesting administrator privileges...")
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error obtaining executable path: %v\n", err)
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
		fmt.Printf("Failed to elevate process (error code %d).\n", ret)
	}
	os.Exit(0)
}
