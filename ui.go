package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modmsvcrt           = windows.NewLazySystemDLL("msvcrt.dll")
	procGetch           = modmsvcrt.NewProc("_getch")
	modkernel32Title    = windows.NewLazySystemDLL("kernel32.dll")
	procSetConsoleTitle = modkernel32Title.NewProc("SetConsoleTitleW")
)

const (
	RED    = "\x1b[31m"
	CYAN   = "\x1b[36m"
	WHITE  = "\x1b[97m"
	YELLOW = "\x1b[33m"
	RST    = "\x1b[0m"
)

// --- Live stats row (replaces the old static Host/User line) -------------
//
// Redraws ONLY row 7 of the main menu screen (the line right after the OS
// version line) every 1 second, using ANSI cursor positioning to overwrite
// just that single line - never a full-screen clear+redraw. This is the
// same technique real-time CLI tools (htop, btop, etc.) use for live
// panels: move cursor to a known row, clear only that line, print new
// content, move cursor back. Everything else on screen is untouched, so
// there is no flicker.
//
// The goroutine is only ever alive while the main menu is being displayed
// and is waiting for input - it is explicitly stopped (via stopCh) before
// any screen transition (Settings, running the cleaning pipeline, exit),
// so it can never race with ClearScreen()/DrawMainMenu() being called
// again or write to a screen that's no longer the main menu.
const liveStatsRow = 7

// StartLiveStatsRow launches a goroutine that repaints row 7 every second
// with "CPU : {%} - MEMORY: {used/total GB} {pct%}". Returns a
// stop channel; close it to cleanly halt the goroutine.
//
// promptRow is the row the "  > " input prompt sits on (always 2 rows
// below the last menu line at the time this is called), so that after
// repainting row 7 we can explicitly park the cursor back there. This is
// used instead of ANSI cursor save/restore (\x1b[s / \x1b[u), since this
// write happens concurrently with a blocking stdin read on the main
// goroutine and we want the cursor's final position to be deterministic
// rather than depend on save/restore semantics under concurrent access.
func StartLiveStatsRow(promptRow int) chan struct{} {
	stopCh := make(chan struct{})

	go func() {
		defer func() {
			// Never let a stats-refresh panic take down the whole app;
			// this is a cosmetic feature, not a critical path.
			_ = recover()
		}()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				stats := GetSystemStats()
				line := fmt.Sprintf(
					"  CPU : %s%d%%%s  -  MEMORY: %s%.2f/%.2f GB%s %s%d%%%s",
					RED, stats.CpuLoad, RST,
					RED, stats.UsedGB, stats.TotalGB, RST,
					RED, stats.Pct, RST,
				)

				// Jump to the fixed stats row, clear only that line, print
				// new content, then explicitly return the cursor to the
				// input prompt's row/column so the blocking ReadString on
				// the main goroutine still shows its cursor in the right
				// place. No save/restore - both positions are known and
				// fixed, so we just state them directly each tick.
				fmt.Printf("\x1b[%d;1H\x1b[2K%s\x1b[%d;5H", liveStatsRow, line, promptRow)
			}
		}
	}()

	return stopCh
}

// StopLiveStatsRow signals the goroutine started by StartLiveStatsRow to
// stop. Safe to call even if the row happens to be mid-tick; the select
// in the goroutine picks up stopCh on the next loop iteration.
func StopLiveStatsRow(stopCh chan struct{}) {
	close(stopCh)
}

func EnableVirtualTerminalProcessing() {
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	err = windows.GetConsoleMode(handle, &mode)
	if err != nil {
		return
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	_ = windows.SetConsoleMode(handle, mode)
}

func SetConsoleTitle(title string) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procSetConsoleTitle.Call(uintptr(unsafe.Pointer(titlePtr)))
}

// TypeLine prints a line, then pauses briefly. Used only for step headers
// and summary banners so the console feels like it's "revealing" progress,
// without slowing down the actual fast file deletion or RAM stat output.
func TypeLine(s string, delay time.Duration) {
	fmt.Println(s)
	time.Sleep(delay)
}

func ClearScreen() {
	fmt.Print("\x1b[H\x1b[2J\x1b[3J")
}

func GetOSVersionParts() (string, string) {
	out, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return "Microsoft Windows ", "[Version 10.0.0.0]"
	}
	verStr := strings.TrimSpace(string(out))
	bracketIdx := strings.Index(verStr, "[")
	if bracketIdx != -1 {
		p1 := verStr[:bracketIdx]
		p2 := verStr[bracketIdx:]
		p2 = strings.ReplaceAll(p2, "]", "")
		p2 = strings.ReplaceAll(p2, "[", "")
		return p1, p2
	}
	return verStr, ""
}

func GetHostAndUser() (string, string) {
	host, _ := os.Hostname()
	user := os.Getenv("USERNAME")
	if user == "" {
		user = "User"
	}
	return host, user
}

func ReadSingleKey() rune {
	r1, _, _ := procGetch.Call()
	return rune(r1)
}

func GetCfgDispName(index int) string {
	switch index {
	case 1:
		return "[1]  Windows Temp folder     "
	case 2:
		return "[2]  User Temp folder        "
	case 3:
		return "[3]  Prefetch folder         "
	case 4:
		return "[4]  Windows Error Reports   "
	case 5:
		return "[5]  Delivery Optimization   "
	case 6:
		return "[6]  Windows Update Cache    "
	case 7:
		return "[7]  Windows Log Files       "
	case 8:
		return "[8]  Windows Installer Temp  "
	case 9:
		return "[9]  DNS Cache Flush         "
	case 10:
		return "[10] RAM Optimization        "
	case 11:
		return "[11] Empty Recycle Bin       "
	default:
		return ""
	}
}

// DrawMainMenu prints the main menu and returns the screen row number
// where the "  > " input prompt will be printed immediately after this
// call returns (used by main.go to start the live stats goroutine with
// the correct row to return the cursor to after each repaint). The row
// count is computed as the menu is drawn, not hand-counted, so it stays
// correct even if lines are added/removed from this function later.
func DrawMainMenu(cfg *Config, osP1, osP2, host, user string) int {
	row := 0
	pln := func(a ...interface{}) {
		fmt.Println(a...)
		row++
	}
	pf := func(format string, a ...interface{}) {
		fmt.Printf(format, a...)
		row++
	}

	pln()
	pln("+---------------------------------------+")
	pln("            QUIESCE")
	pln("+---------------------------------------+")
	pf("  %sSibtainOcn ~ %sQuiesce v2.1%s\n", WHITE, RED, RST)
	pf("  %s%s%s%s%s\n", WHITE, osP1, RED, osP2, RST)
	// Fetch real stats immediately so the first frame is never blank.
	initStats := GetSystemStats()
	statsRowNum := row + 1
	pf("  CPU : %s%d%%%s  -  MEMORY: %s%.2f/%.2f GB%s %s%d%%%s\n",
		RED, initStats.CpuLoad, RST,
		RED, initStats.UsedGB, initStats.TotalGB, RST,
		RED, initStats.Pct, RST,
	)
	pln("+---------------------------------------+")
	pln()
	pf("%sThis will clean:%s\n", WHITE, RST)

	for i := 1; i <= 11; i++ {
		disp := GetCfgDispName(i)
		val := cfg.GetVal(i)
		if val == 1 {
			pf("  %s : %s[ON]%s\n", disp, RED, RST)
		} else {
			pf("  %s : %s[OFF]%s\n", disp, CYAN, RST)
		}
	}

	// Deep Cleanup — inline, right below Recycle Bin
	if cfg.DeepCleanup == 1 {
		pf("  [12] Deep Cleanup - %sRUN ONCE%s  : %s[ON]%s\n", RED, RST, RED, RST)
	} else {
		pf("  [12] Deep Cleanup - %sRUN ONCE%s  : %s[OFF]%s\n", RED, RST, CYAN, RST)
	}

	pln()
	pln("+---------------------------------------+")
	pf("  Press %sENTER%s to Run  |  Press %sF%s to Configure\n", WHITE, RST, WHITE, RST)
	pln("+---------------------------------------+")
	pln()

	// Sanity check: statsRowNum must match the liveStatsRow constant used
	// by StartLiveStatsRow's ANSI positioning. If someone edits the lines
	// above liveStatsRow without updating the constant, this will be caught
	// immediately in testing rather than silently corrupting the wrong line.
	if statsRowNum != liveStatsRow {
		fmt.Printf("\x1b[33m[WARN]\x1b[0m liveStatsRow constant (%d) doesn't match actual row (%d) - live stats row will be misplaced. Update liveStatsRow in ui.go.\n", liveStatsRow, statsRowNum)
	}

	// The prompt "  > " is printed by main.go immediately after this
	// function returns, with no trailing newline - so it lands on the
	// very next row after everything printed above.
	promptRow := row + 1
	return promptRow
}

func RunSettingsScreen(cfg *Config, configFilePath string) {
	sel := 1

	for {
		ClearScreen()
		fmt.Println()
		fmt.Printf("%s+---------------------------------------+%s\n", WHITE, RST)
		fmt.Printf("%s         CONFIGURE CLEANING OPTIONS%s\n", WHITE, RST)
		fmt.Printf("%s+---------------------------------------+%s\n", WHITE, RST)
		fmt.Printf("  %sW%s = Up  |  %sS%s = Down  |  %sA%s = OFF  |  %sD%s = ON  |  %sE%s = Save\n", WHITE, RST, WHITE, RST, WHITE, RST, WHITE, RST, WHITE, RST)
		fmt.Println()

		for i := 1; i <= 11; i++ {
			disp := GetCfgDispName(i)
			val := cfg.GetVal(i)
			a := "   "
			if sel == i {
				a = fmt.Sprintf(" %s<<<<<%s", YELLOW, RST)
			}

			if val == 1 {
				fmt.Printf("   %s : %s[ON]%s%s\n", disp, RED, RST, a)
			} else {
				fmt.Printf("   %s : %s[OFF]%s%s\n", disp, CYAN, RST, a)
			}
		}

		// Deep Cleanup — inline right after cleaning options (sel=12)
		{
			deepVal := cfg.DeepCleanup
			a := "   "
			if sel == 12 {
				a = fmt.Sprintf(" %s<<<<<%s", YELLOW, RST)
			}
			if deepVal == 1 {
				fmt.Printf("   [12] Deep Cleanup - %sRUN ONCE%s  : %s[ON]%s%s\n", RED, RST, RED, RST, a)
			} else {
				fmt.Printf("   [12] Deep Cleanup - %sRUN ONCE%s  : %s[OFF]%s%s\n", RED, RST, CYAN, RST, a)
			}
		}

		fmt.Println()
		fmt.Printf("%s+---------------------------------------+%s\n", WHITE, RST)
		fmt.Printf("  Press %sE%s to Save & Return\n", WHITE, RST)
		fmt.Printf("%s+---------------------------------------+%s\n", WHITE, RST)
		fmt.Print("  > ")

		ch := ReadSingleKey()
		chStr := strings.ToLower(string(ch))

		switch chStr {
		case "w":
			sel--
			if sel < 1 {
				sel = 12
			}
		case "s":
			sel++
			if sel > 12 {
				sel = 1
			}
		case "a":
			if sel == 12 {
				cfg.DeepCleanup = 0
			} else {
				cfg.SetVal(sel, 0)
			}
		case "d":
			if sel == 12 {
				cfg.DeepCleanup = 1
			} else {
				cfg.SetVal(sel, 1)
			}
		case "e":
			// Save config — DeepCleanup is intentionally NOT saved
			_ = SaveConfig(configFilePath, cfg)
			fmt.Println()
			fmt.Printf("  %s[SAVED]%s Configuration saved.\n", WHITE, RST)
			time.Sleep(1 * time.Second)
			ClearScreen()
			return
		}
	}
}

func PrintSummary(cfg *Config, results StepResults) {
	fmt.Println()
	fmt.Println("+---------------------------------------+")
	fmt.Println("             CLEANING SUMMARY")
	fmt.Println("+---------------------------------------+")
	fmt.Println()
	fmt.Printf("  %sTotal items cleaned : %d%s\n", RED, results.TotalDeleted, RST)
	fmt.Printf("  Items skipped       : %d (in use/protected)\n", results.TotalFailed)
	fmt.Println()

	steps := []struct {
		idx  int
		name string
		cfg  int
		isSp bool
	}{
		{1, "Windows Temp          ", cfg.WinTemp, false},
		{2, "User Temp             ", cfg.UserTemp, false},
		{3, "Prefetch              ", cfg.Prefetch, false},
		{4, "Error Reports         ", cfg.ErrorReports, false},
		{5, "Delivery Optimization ", cfg.DeliveryOpt, false},
		{6, "Windows Update Cache  ", cfg.WinUpdate, false},
		{7, "Windows Log Files     ", cfg.LogFiles, false},
		{8, "Installer Temp        ", cfg.InstallerTemp, false},
		{9, "DNS Cache             ", cfg.DnsFlush, true},
		{10, "RAM Optimization      ", cfg.RamOptimize, true},
		{11, "Recycle Bin           ", cfg.RecycleBin, true},
	}

	for _, s := range steps {
		label := fmt.Sprintf("[%d]", s.idx)
		if s.cfg == 1 {
			if !s.isSp {
				fmt.Printf("  %-5s%s : %d items\n", label, s.name, results.StepCounts[s.idx])
			} else if s.idx == 9 {
				fmt.Printf("  %-5s%s : flushed\n", label, s.name)
			} else if s.idx == 10 {
				if results.RamFreedMB > 0 {
					fmt.Printf("  %-5s%s : %d MB freed\n", label, s.name, results.RamFreedMB)
				} else if results.RamFreedMB == 0 {
					fmt.Printf("  %-5s%s : 0 MB (already optimal)\n", label, s.name)
				} else {
					fmt.Printf("  %-5s%s : %s%d MB (background app allocated)%s\n", label, s.name, CYAN, results.RamFreedMB, RST)
				}
			} else if s.idx == 11 {
				if results.RecycleBinFailed {
					fmt.Printf("  %-5s%s : %sfailed%s\n", label, s.name, CYAN, RST)
				} else if results.RecycleBinBytes > 0 {
					fmt.Printf("  %-5s%s : %s freed\n", label, s.name, FormatBytesHuman(results.RecycleBinBytes))
				} else {
					fmt.Printf("  %-5s%s : already empty\n", label, s.name)
				}
			}
		} else {
			fmt.Printf("  %-5s%s : %sSKIPPED%s\n", label, s.name, CYAN, RST)
		}
	}

	// Deep Cleanup summary line
	if results.DeepCleanupRan {
		if results.DeepCleanupFailed {
			fmt.Printf("  [12] Deep Cleanup - %sRUN ONCE%s : %spartial/failed%s\n", RED, RST, CYAN, RST)
		} else {
			fmt.Printf("  [12] Deep Cleanup - %sRUN ONCE%s : %scompleted%s\n", RED, RST, RED, RST)
		}
	} else {
		fmt.Printf("  [12] Deep Cleanup - %sRUN ONCE%s : %sSKIPPED%s\n", RED, RST, CYAN, RST)
	}

	fmt.Println()
	fmt.Println("+---------------------------------------+")
	fmt.Println("  ALL DONE - Performance Boost Complete!")
	fmt.Println("+---------------------------------------+")
	fmt.Println()
	fmt.Println("  SibtainOcn ~ Quiesce v2.1")
	fmt.Println()
}
