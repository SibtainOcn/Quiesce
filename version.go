package main

// Identity and version information baked into every compiled binary.
//
// The constants below are compiled into the executable as literal bytes, so
// they survive renaming the file, stripping the LICENSE, and re-uploading it
// elsewhere. Anyone can recover them from a suspect binary with:
//
//	strings qc.exe | findstr /i SibtainOcn
//
// This is a forensic marker, not a legal control - the license in LICENSE is
// what governs use. It just makes authorship of a rebadged copy easy to prove.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

const (
	AppName     = "Quiesce"
	AppShort    = "qc"
	Author      = "SibtainOcn"
	Repo        = "https://github.com/SibtainOcn/Quiesce"
	LicenseName = "Quiesce Source-Available License (QSAL) v1.0"
	Copyright   = "Copyright (c) 2026 SibtainOcn"
	ContactURL  = "https://github.com/SibtainOcn"
)

// Version/Commit/BuildDate are overridable at build time:
//
//	go build -ldflags "-X main.Version=2.2.0 -X main.Commit=$(git rev-parse --short HEAD)" -o qc.exe
//
// Version carries a real default so an unflagged `go build` still produces a
// correctly-labelled binary rather than "dev".
var (
	Version   = "2.2.0"
	Commit    = ""
	BuildDate = ""
)

// init fills Commit/BuildDate from the VCS stamps the Go toolchain records
// automatically when building inside a git work tree, so a plain `go build`
// still carries provenance even without -ldflags.
func init() {
	if Commit != "" && BuildDate != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "" && len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		case "vcs.time":
			if BuildDate == "" {
				BuildDate = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				Commit += "-dirty"
			}
		}
	}
}

// VersionLine is the single-line identity shown in the menu banner, e.g.
// "SibtainOcn ~ Quiesce v2.2.0".
func VersionLine() string {
	return fmt.Sprintf("%s ~ %s v%s", Author, AppName, Version)
}

// versionDetail renders "v2.2.0 (a1b2c3d, 2026-08-16T...)" with whichever
// build stamps are actually present.
func versionDetail() string {
	var parts []string
	if Commit != "" {
		parts = append(parts, Commit)
	}
	if BuildDate != "" {
		parts = append(parts, BuildDate)
	}
	if len(parts) == 0 {
		return "v" + Version
	}
	return fmt.Sprintf("v%s (%s)", Version, strings.Join(parts, ", "))
}

// SelfSHA256 returns the SHA-256 of the running executable, so a user can
// verify their copy against the checksum published with the official release
// without needing any other tool.
func SelfSHA256() string {
	path, err := os.Executable()
	if err != nil {
		return "unavailable"
	}
	f, err := os.Open(path)
	if err != nil {
		return "unavailable"
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PrintAbout writes the full identity block. Printed by --version/--about,
// which run WITHOUT elevating - checking what a binary claims to be should
// never require handing it Administrator.
func PrintAbout() {
	fmt.Println()
	fmt.Printf("  %s %s\n", AppName, versionDetail())
	fmt.Printf("  Author     : %s\n", Author)
	fmt.Printf("  Repository : %s\n", Repo)
	fmt.Printf("  License    : %s\n", LicenseName)
	fmt.Printf("  %s\n", Copyright)
	fmt.Printf("  Built with : %s (%s/%s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  SHA-256    : %s\n", SelfSHA256())
	fmt.Println()
	fmt.Println("  Compare the SHA-256 above against the checksum published with the")
	fmt.Printf("  official release at %s/releases\n", Repo)
	fmt.Println("  A mismatch means this binary is NOT an official build.")
	fmt.Println()
}

// PrintHelp lists the command-line flags. The interactive menu is the primary
// interface; these flags exist for identity checks and scripting.
func PrintHelp() {
	fmt.Println()
	fmt.Printf("  %s %s - Windows system cleaner and RAM optimizer\n", AppName, versionDetail())
	fmt.Println()
	fmt.Printf("  Usage: %s [flag]\n", AppShort)
	fmt.Println()
	fmt.Println("    (no flag)          Launch the interactive menu (elevates via UAC)")
	fmt.Println("    -v, --version      Print version, author, repository and license")
	fmt.Println("        --about        Same as --version")
	fmt.Println("    -h, --help         Show this help")
	fmt.Println()
}

// HandleCLIFlags processes any command-line flag and reports whether the
// program should exit immediately. Called before EnsureAdmin so informational
// flags never trigger a UAC prompt.
func HandleCLIFlags(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "-v", "--version", "-version", "version", "--about", "about":
		PrintAbout()
		return true
	case "-h", "--help", "-help", "help", "/?":
		PrintHelp()
		return true
	}
	return false
}
