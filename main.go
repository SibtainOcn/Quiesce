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
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	// Top-level crash guard: if anything panics the console stays open
	// so the user can read the error instead of the window vanishing.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n\x1b[31m[FATAL]\x1b[0m %s\n", Tf("common.fatal", r))
			fmt.Print(T("common.press_enter"))
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
	EnableUTF8Console()

	// The config path is resolved, and the UI language settled, before ANY
	// output. InitLanguage needs the config file for its LANGUAGE= override,
	// and --version/--help and the elevation prompt all print before
	// LoadConfig would otherwise run.
	configFilePath := ConfigFilePath()
	InitLanguage(configFilePath)

	if HandleCLIFlags(os.Args[1:]) {
		return
	}

	// Ensure Administrator privilege
	EnsureAdmin()

	// Re-apply console setup: EnsureAdmin relaunches into a fresh console,
	// so ANSI colors and the UTF-8 code page have to be enabled again.
	EnableVirtualTerminalProcessing()
	EnableUTF8Console()

	// Set a clean console window title instead of showing the exe's file path
	SetConsoleTitle("Quiesce")

	// Load configuration
	cfg, err := LoadConfig(configFilePath)
	if err != nil {
		fmt.Printf("%s\n", Tf("common.config_warn", err))
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
		fmt.Print(T("common.press_enter"))
		WaitForEnter()
		os.Exit(0)
	}
}
