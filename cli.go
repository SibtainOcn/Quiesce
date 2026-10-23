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

// Direct run commands: `qc temp-1 deep bin` runs just those steps without
// the menu. The saved config is only read, never written.

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// cliCommand is one run command. step is the menu step it shows as in help;
// sub is the RAM sub-option index (101-104), or 0.
type cliCommand struct {
	name    string
	aliases []string
	step    int
	sub     int
	apply   func(c *Config)
}

// cliAll runs the saved configuration.
const cliAll = "all"

var cliCommands = []cliCommand{
	{name: "temp", step: 1, apply: func(c *Config) { c.WinTemp, c.UserTemp = 1, 1 }},
	{name: "temp-1", step: 1, apply: func(c *Config) { c.WinTemp = 1 }},
	{name: "temp-2", step: 2, apply: func(c *Config) { c.UserTemp = 1 }},
	{name: "prefetch", step: 3, apply: func(c *Config) { c.Prefetch = 1 }},
	{name: "errors", aliases: []string{"wer"}, step: 4, apply: func(c *Config) { c.ErrorReports = 1 }},
	{name: "delivery", step: 5, apply: func(c *Config) { c.DeliveryOpt = 1 }},
	{name: "win-update", aliases: []string{"wu"}, step: 6, apply: func(c *Config) { c.WinUpdate = 1 }},
	{name: "logs", step: 7, apply: func(c *Config) { c.LogFiles = 1 }},
	{name: "installer", step: 8, apply: func(c *Config) { c.InstallerTemp = 1 }},
	{name: "dns", step: 9, apply: func(c *Config) { c.DnsFlush = 1 }},
	{name: "ram", step: 10, apply: func(c *Config) {
		c.RamOptimize = 1
		c.RamFlushModified, c.RamPurgeStandby, c.RamFileCache, c.RamTrimWorkingSets = 1, 1, 1, 1
	}},
	{name: "ram-1", step: 10, sub: 101, apply: func(c *Config) { c.RamOptimize, c.RamFlushModified = 1, 1 }},
	{name: "ram-2", step: 10, sub: 102, apply: func(c *Config) { c.RamOptimize, c.RamPurgeStandby = 1, 1 }},
	{name: "ram-3", step: 10, sub: 103, apply: func(c *Config) { c.RamOptimize, c.RamFileCache = 1, 1 }},
	{name: "ram-4", step: 10, sub: 104, apply: func(c *Config) { c.RamOptimize, c.RamTrimWorkingSets = 1, 1 }},
	{name: "bin", aliases: []string{"recycle"}, step: 11, apply: func(c *Config) { c.RecycleBin = 1 }},
	{name: "deep", step: 12, apply: func(c *Config) { c.DeepCleanup = 1 }},
	// no-ram runs steps 1-9: everything except RAM, Recycle Bin and Deep Cleanup.
	{name: "no-ram", step: 1, apply: func(c *Config) {
		c.WinTemp, c.UserTemp, c.Prefetch, c.ErrorReports = 1, 1, 1, 1
		c.DeliveryOpt, c.WinUpdate, c.LogFiles, c.InstallerTemp, c.DnsFlush = 1, 1, 1, 1, 1
	}},
}

func findCLICommand(token string) (cliCommand, bool) {
	for _, c := range cliCommands {
		if c.name == token {
			return c, true
		}
		for _, a := range c.aliases {
			if a == token {
				return c, true
			}
		}
	}
	return cliCommand{}, false
}

// ParseRunArgs validates run commands and returns their canonical names,
// in order and without duplicates. It returns nil for no arguments.
func ParseRunArgs(args []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range args {
		tok := strings.ToLower(strings.TrimSpace(raw))
		if tok == "" {
			continue
		}
		name := tok
		if tok != cliAll {
			c, ok := findCLICommand(tok)
			if !ok {
				return nil, errors.New(Tf("cli.unknown", raw))
			}
			name = c.name
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// BuildRunConfig makes the config for a direct run. Every step starts OFF
// unless "all" is given, which keeps the saved selection. Display options
// (output style, log file, language) always come from the saved config.
func BuildRunConfig(saved *Config, cmds []string) *Config {
	cfg := *saved
	cfg.DeepCleanup = 0
	cfg.directRun = true

	hasAll := false
	for _, n := range cmds {
		if n == cliAll {
			hasAll = true
		}
	}
	if !hasAll {
		cfg.WinTemp, cfg.UserTemp, cfg.Prefetch, cfg.ErrorReports = 0, 0, 0, 0
		cfg.DeliveryOpt, cfg.WinUpdate, cfg.LogFiles, cfg.InstallerTemp = 0, 0, 0, 0
		cfg.DnsFlush, cfg.RamOptimize, cfg.RecycleBin = 0, 0, 0
		cfg.RamFlushModified, cfg.RamPurgeStandby, cfg.RamFileCache, cfg.RamTrimWorkingSets = 0, 0, 0, 0
	}

	// Any ram-N picks exactly the listed RAM sub-options, even with "all".
	for _, n := range cmds {
		if c, ok := findCLICommand(n); ok && c.sub != 0 {
			cfg.RamFlushModified, cfg.RamPurgeStandby, cfg.RamFileCache, cfg.RamTrimWorkingSets = 0, 0, 0, 0
			break
		}
	}
	for _, n := range cmds {
		if c, ok := findCLICommand(n); ok {
			c.apply(&cfg)
		}
	}
	return &cfg
}

// RunCLI runs a direct command and returns the process exit code.
func RunCLI(saved *Config, cmds []string) int {
	cfg := BuildRunConfig(saved, cmds)
	results := RunCleaningPipeline(cfg)
	PrintSummary(cfg, results)

	// A window opened just for this run (double-click or UAC relaunch) would
	// vanish with the summary, so wait there. In a shared terminal, return.
	if ownsConsoleWindow() {
		fmt.Print(T("common.press_enter"))
		WaitForEnter()
	}
	if results.Aborted {
		return 1
	}
	return 0
}

// ownsConsoleWindow reports whether this process is alone on its console.
func ownsConsoleWindow() bool {
	proc := modkernel32Lang.NewProc("GetConsoleProcessList")
	var ids [4]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return n == 1
}

// printCLICommands lists the run commands for --help.
func printCLICommands() {
	fmt.Printf("  %s\n", T("help.commands"))
	fmt.Println()
	row := helpRow
	row(cliAll, T("help.cmd_all"))
	for _, c := range cliCommands {
		name := c.name
		if len(c.aliases) > 0 {
			name += ", " + strings.Join(c.aliases, ", ")
		}
		var desc string
		switch {
		case c.sub != 0:
			desc = T(fmt.Sprintf("sub.%d.name", c.sub))
		case c.name == "temp":
			desc = T("help.cmd_temp")
		case c.name == "ram":
			desc = T("help.cmd_ram")
		case c.name == "no-ram":
			desc = T("help.cmd_noram")
		default:
			desc = T(fmt.Sprintf("step.%d.short", c.step))
		}
		row(name, desc)
	}
	fmt.Println()
	fmt.Printf("  %s\n", T("help.elevate"))
	fmt.Println()
}
