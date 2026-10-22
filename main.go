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
	"errors"
	"fmt"
	"os"
	"strings"
)

func main() {
	// Top-level crash guard: if anything panics the console stays open
	// so the user can read the error instead of the window vanishing.
	defer func() {
		if r := recover(); r != nil {
			// Show the cursor again: the live screen hides it while drawing.
			fmt.Print("\x1b[?25h")
			fmt.Printf("\n\x1b[31m[FATAL]\x1b[0m %s\n", Tf("common.fatal", r))
			fmt.Print(T("common.press_enter"))
			bufio.NewReader(os.Stdin).ReadString('\n')
			RestoreConsoleMode()
			os.Exit(1)
		}
	}()

	// The elevated copy of a direct command joins the caller's terminal
	// before any console setup, so that setup applies to the right console.
	args := os.Args[1:]
	attachPID, rest, isAttached, attachErr := splitAttachArgs(args)
	if attachErr != nil {
		os.Exit(2)
	}
	sharedConsole := false
	if isAttached {
		sharedConsole = attachToCaller(attachPID)
		args = rest
	}

	// Informational flags are handled BEFORE elevation: checking what a
	// binary claims to be (--version prints author, repo, license and the
	// executable's own SHA-256) should never require granting it Admin.
	EnableVirtualTerminalProcessing()
	EnableUTF8Console()
	DisableQuickEditMode()

	// Covers the paths that return normally rather than calling os.Exit -
	// notably --version and --help, which exit before the menu ever opens.
	// Calls made on the os.Exit paths are separate and harmless to repeat.
	defer RestoreConsoleMode()

	// The config path is resolved, and the UI language settled, before ANY
	// output. InitLanguage needs the config file for its LANGUAGE= override,
	// and --version/--help and the elevation prompt all print before
	// LoadConfig would otherwise run.
	configFilePath := ConfigFilePath()
	InitLanguage(configFilePath)
	RemoveOldBinary()

	// Updating needs Admin only when qc.exe's folder does, so it runs here.
	if len(args) == 1 && isUpdateFlag(args[0]) {
		code := RunSelfUpdate(isAttached)
		RestoreConsoleMode()
		os.Exit(code)
	}

	// The user PATH needs no Admin, and writing it elevated could pick the
	// wrong account's settings.
	if !isAttached && len(args) == 1 {
		var run func() int
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "env":
			run = RunPathCommand
		case "repo", "github":
			run = RunRepoCommand
		}
		if run != nil {
			code := run()
			RestoreConsoleMode()
			os.Exit(code)
		}
	}

	if !isAttached && HandleCLIFlags(args) {
		return
	}

	// Run commands are checked before elevation, so a typo never asks for Admin.
	runCmds, err := ParseRunArgs(args)
	if err == nil && isAttached && len(runCmds) == 0 {
		err = errors.New(Tf("cli.unknown", attachFlag))
	}
	if err != nil {
		fmt.Printf("\n  \x1b[31m%s\x1b[0m\n  %s\n\n", err, Tf("cli.see_help", AppShort))
		RestoreConsoleMode()
		os.Exit(2)
	}

	// From a normal terminal, a direct command runs elevated in this same
	// terminal instead of a new window.
	if len(runCmds) > 0 && !IsAdmin() {
		code := RunElevatedInTerminal(runCmds)
		RestoreConsoleMode()
		os.Exit(code)
	}

	EnsureAdmin()

	// Re-apply console setup: EnsureAdmin relaunches into a fresh console, so
	// ANSI colors, the UTF-8 code page and the QuickEdit fix all have to be
	// applied again to the new console.
	EnableVirtualTerminalProcessing()
	EnableUTF8Console()
	DisableQuickEditMode()

	// Set a clean console window title instead of showing the exe's file
	// path. The version is included so a screenshot always says which build
	// it came from.
	// The caller's terminal keeps its own title.
	if !sharedConsole {
		SetConsoleTitle(AppName + " v" + Version)
	}

	// From here on Ctrl+C and closing the window restore any stopped
	// service and the console before exiting.
	InstallShutdownHandler()

	// Load configuration
	_, statErr := os.Stat(configFilePath)
	firstRun := os.IsNotExist(statErr)
	cfg, err := LoadConfig(configFilePath)
	if err != nil {
		fmt.Printf("%s\n", Tf("common.config_warn", err))
	}
	if firstRun && stdinIsConsole() {
		AskAutoUpdate(cfg)
		_ = SaveConfig(configFilePath, cfg)
		OfferAddToPath()
	}
	StartUpdateCheck(cfg)

	if len(runCmds) > 0 {
		code := RunCLI(cfg, runCmds)
		RestoreConsoleMode()
		os.Exit(code)
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
		RestoreConsoleMode()
		os.Exit(0)
	}
}
