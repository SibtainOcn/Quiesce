// Quiesce - Windows system cleaner and RAM optimizer.
//
// Copyright (c) 2026 SibtainOcn
// https://github.com/SibtainOcn/Quiesce
//
// Licensed under the Quiesce Source-Available License (QSAL) v1.0.
// Source-available for transparency and personal use. Redistribution of the
// source or of self-built binaries, and reuse of this code in other projects,
// require prior written permission from the author. See LICENSE.

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Top-level crash guard: if anything panics the console stays open
	// so the user can read the error instead of the window vanishing.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n\x1b[31m[FATAL]\x1b[0m Unexpected error: %v\n", r)
			fmt.Print("Press ENTER to exit...")
			bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(1)
		}
	}()

	// Informational flags are handled BEFORE elevation: checking what a
	// binary claims to be (--version prints author, repo, license and the
	// executable's own SHA-256) should never require granting it Admin.
	// EnsureAdmin relaunches without arguments, so a flag would be lost
	// across the elevation anyway.
	EnableVirtualTerminalProcessing()
	if HandleCLIFlags(os.Args[1:]) {
		return
	}

	// Ensure Administrator privilege
	EnsureAdmin()

	// Enable ANSI terminal color support in Windows console
	EnableVirtualTerminalProcessing()

	// Set a clean console window title instead of showing the exe's file path
	SetConsoleTitle("Quiesce")

	// Determine configuration file path alongside executable
	exePath, err := os.Executable()
	var exeDir string
	if err == nil {
		exeDir = filepath.Dir(exePath)
	} else {
		exeDir = "."
	}
	configFilePath := filepath.Join(exeDir, "cleaner_config.dat")

	// Load configuration
	cfg, err := LoadConfig(configFilePath)
	if err != nil {
		fmt.Printf("Warning loading config: %v\n", err)
	}

	osP1, osP2 := GetOSVersionParts()
	host, user := GetHostAndUser()


	for {
		ClearScreen()
		promptRow := DrawMainMenu(cfg, osP1, osP2, host, user)

		// The prompt is printed BEFORE the live stats row starts: the stats
		// goroutine parks the cursor at the prompt's row/column after every
		// repaint, so the prompt text must already be on screen or its first
		// paint would push the prompt out of column 1.
		fmt.Print("  > ")

		// Live CPU/Memory stats row - painted once immediately, then every
		// second. Explicitly stopped before ANY transition away from this
		// screen - Settings, running the cleaner, or exiting - so it can
		// never write to a screen that's no longer the main menu.
		statsStopCh := StartLiveStatsRow(promptRow)

		// Single-key input loop: only ENTER (run) and F (settings) are
		// valid. Everything else is silently ignored — no echo, no cursor
		// movement, no garbage on screen.
		var action string
		for {
			ch := ReadSingleKey()
			if ch == 13 { // ENTER key
				action = "run"
				break
			}
			lower := strings.ToLower(string(ch))
			if lower == "f" {
				action = "settings"
				break
			}
			// Ignore all other keys silently
		}

		StopLiveStatsRow(statsStopCh)

		if action == "settings" {
			RunSettingsScreen(cfg, configFilePath)
			continue
		}

		// Proceed to run cleaning
		results := RunCleaningPipeline(cfg)
		PrintSummary(cfg, results)

		// WaitForEnter flushes anything typed during the run first, so ENTER
		// presses made while cleaning was in progress can't close the window
		// before the summary has been read.
		fmt.Print("Press ENTER to exit...")
		WaitForEnter()
		os.Exit(0)
	}
}
