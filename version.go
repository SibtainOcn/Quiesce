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

// Identity and version information baked into every compiled binary.
//
// The constants below are compiled into the executable as literal bytes, so
// they survive renaming the file and re-uploading it elsewhere. Anyone can
// recover them from a suspect binary with:
//
//	strings qc.exe | findstr /i SibtainOcn
//
// Under the GPL this is attribution and provenance, not a restriction:
// redistributing and modifying Quiesce is explicitly allowed. What the GPL
// does require is that recipients get the same freedoms and the source, and
// that modified versions are marked as changed - these strings make the
// original authorship and version easy to establish.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"quiesce/locales"
)

const (
	AppName     = "Quiesce"
	AppShort    = "qc"
	Author      = "SibtainOcn"
	Repo        = "https://github.com/SibtainOcn/Quiesce"
	LicenseName = "GNU General Public License v3.0 or later (GPL-3.0-or-later)"
	LicenseURL  = "https://www.gnu.org/licenses/gpl-3.0.html"
	Copyright   = "Copyright (C) 2026 SibtainOcn"
	ContactURL  = "https://github.com/SibtainOcn"
)

// Version and Commit are overridable at build time:
//
//	go build -ldflags "-X main.Version=2.3.0" -o qc.exe
//
// Version carries a real default so an unflagged `go build` still produces a
// correctly-labelled binary rather than "dev". Build timestamps are
// deliberately not recorded or shown - they add noise to every version check
// and make otherwise identical builds look different.
var (
	Version = "2.3.1"
	Commit  = ""
)

// init fills Commit from the VCS stamp the Go toolchain records automatically
// when building inside a git work tree, so a plain `go build` still identifies
// exactly which source a binary came from. That is what a bug report needs.
func init() {
	if Commit != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		case "vcs.modified":
			if s.Value == "true" {
				Commit += "-dirty"
			}
		}
	}
}

// VersionLine is the single-line identity shown in the menu banner, e.g.
// "SibtainOcn ~ Quiesce v2.3.0".
func VersionLine() string {
	return fmt.Sprintf("%s ~ %s v%s", Author, AppName, Version)
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

// PrintAbout writes the identity block. Printed by --version/--about, which
// run WITHOUT elevating - checking what a binary claims to be should never
// require handing it Administrator.
//
// Kept to six lines. Everything here answers a question someone actually has:
// which build is this, who wrote it, where does it come from, what may I do
// with it, and is my copy the official one. The GPL notice is condensed to the
// one line GPLv3 asks interactive programs to show.
func PrintAbout() {
	build := ""
	if Commit != "" {
		build = "  (" + Commit + ")"
	}

	fmt.Println()
	fmt.Printf("  %s v%s%s\n", AppName, Version, build)
	fmt.Printf("  %s  -  %s\n", Author, Repo)
	fmt.Printf("  %s\n", T(locales.AboutGpl))
	fmt.Println()
	fmt.Printf("  SHA-256  %s\n", SelfSHA256())
	fmt.Printf("  %s\n", TD(locales.AboutCompare, map[string]any{"Repo": Repo}))
	fmt.Println()
}

// PrintHelp lists the command-line flags. The interactive menu is the primary
// interface; these flags exist for identity checks and scripting.
func PrintHelp() {
	fmt.Println()
	fmt.Printf("  %s v%s - %s\n", AppName, Version, T(locales.HelpTagline))
	fmt.Println()
	fmt.Printf("  %s\n", TD(locales.HelpUsage, map[string]any{"App": AppShort}))
	fmt.Println()
	fmt.Printf("    %-22s%s\n", "(no flag)", T(locales.HelpNoFlag))
	fmt.Printf("    %-22s%s\n", "-v, --version", T(locales.HelpVersionFlag))
	fmt.Printf("    %-22s%s\n", "-h, --help", T(locales.HelpHelpFlag))
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
