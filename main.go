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

		// Live CPU/Memory stats row starts updating only once the menu is
		// fully drawn (so its first tick doesn't race the initial print),
		// and is explicitly stopped before ANY transition away from this
		// screen - Settings, running the cleaner, or exiting - so it can
		// never write to a screen that's no longer the main menu.
		statsStopCh := StartLiveStatsRow(promptRow)

		fmt.Print("  > ")

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

		fmt.Print("Press ENTER to exit...")
		ReadSingleKey()
		os.Exit(0)
	}
}
