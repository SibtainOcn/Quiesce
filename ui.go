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

	procFlushConsoleInputBuffer = modkernel32Title.NewProc("FlushConsoleInputBuffer")
	procSetConsoleOutputCP      = modkernel32Title.NewProc("SetConsoleOutputCP")
)

// CP_UTF8 is the Windows code page identifier for UTF-8.
const CP_UTF8 = 65001

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
// Both the main menu and the settings screen reserve row 2 (directly above
// the first "+---+" box line) for the live stats, so a single constant and a
// single painter cover both screens.
const liveStatsRow = 2

// liveStats is the handle to a running stats-row painter. It carries a done
// channel so StopLiveStatsRow can WAIT for the goroutine to actually exit -
// without that, close(stop) returns immediately while the goroutine may still
// be inside a paint, and that stray write lands on whatever screen was drawn
// next (it used to overwrite the first row of the settings list).
type liveStats struct {
	stop chan struct{}
	done chan struct{}
}

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
func StartLiveStatsRow(promptRow int) *liveStats {
	ls := &liveStats{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	go func() {
		// done is closed last so Stop can't return while a paint is
		// still in flight.
		defer close(ls.done)
		defer func() {
			// Never let a stats-refresh panic take down the whole app;
			// this is a cosmetic feature, not a critical path.
			_ = recover()
		}()

		// Paint once immediately. DrawMainMenu leaves the stats row blank so
		// there is exactly ONE stats line on screen - this live one - rather
		// than a static line that a later tick paints over. Without this
		// first paint the row would sit empty for a full second.
		paint := func() {
			stats := GetSystemStats()
			line := fmt.Sprintf(
				"  "+T("menu.cpu")+" : %s%d%%%s  -  "+T("menu.memory")+": %s%.2f/%.2f GB%s %s%d%%%s",
				RED, stats.CpuLoad, RST,
				RED, stats.UsedGB, stats.TotalGB, RST,
				RED, stats.Pct, RST,
			)

			// Jump to the fixed stats row, clear only that line, print
			// new content, then explicitly return the cursor to the
			// input prompt's row/column so the blocking read on the main
			// goroutine still shows its cursor in the right place. No
			// save/restore - both positions are known and fixed, so we
			// just state them directly each tick.
			fmt.Printf("\x1b[%d;1H\x1b[2K%s\x1b[%d;5H", liveStatsRow, line, promptRow)
		}

		paint()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ls.stop:
				return
			case <-ticker.C:
				// A tick and a stop can become ready in the same instant,
				// and select picks between ready cases at random - so
				// re-check stop before painting, or the final tick can
				// still write to a screen that's already been replaced.
				select {
				case <-ls.stop:
					return
				default:
				}
				paint()
			}
		}
	}()

	return ls
}

// StopLiveStatsRow stops the painter and BLOCKS until it has actually exited,
// guaranteeing no further writes to the screen once this returns. Callers rely
// on that before drawing a different screen.
func StopLiveStatsRow(ls *liveStats) {
	if ls == nil {
		return
	}
	close(ls.stop)
	<-ls.done
}

// EnableUTF8Console switches the console's output code page to UTF-8.
//
// Go source and therefore every translated string is UTF-8, but a Windows
// console defaults to a legacy OEM code page (437/850/852...) that has no
// idea what to do with multi-byte sequences. Without this, Spanish accents
// and inverted punctuation render as mojibake ("OptimizaciÃ³n"), which is why
// translations must NOT work around the problem by stripping accents.
//
// Failure is ignored: on the rare console where this is refused, output
// degrades to garbled accents rather than the app refusing to start. The box
// drawing and menu keys are plain ASCII either way.
func EnableUTF8Console() {
	if procSetConsoleOutputCP.Find() != nil {
		return
	}
	procSetConsoleOutputCP.Call(uintptr(CP_UTF8))
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

// FlushKeyBuffer discards every keystroke sitting in the console input queue.
//
// Keys pressed WHILE the cleaner is running don't disappear - Windows queues
// them, and the next read consumes them instantly. So mashing ENTER during a
// run used to fly straight through the "Press ENTER to exit" prompt and close
// the window before the summary could be read. Flushing immediately before
// that prompt means only a press made AFTER the run finished can dismiss it.
func FlushKeyBuffer() {
	if procFlushConsoleInputBuffer.Find() != nil {
		return
	}
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return
	}
	procFlushConsoleInputBuffer.Call(uintptr(h))
}

// WaitForEnter blocks until ENTER is actually pressed, ignoring every other
// key. Pending keystrokes are flushed first, so this cannot be satisfied by
// something typed earlier.
func WaitForEnter() {
	FlushKeyBuffer()
	for {
		if ReadSingleKey() == 13 {
			return
		}
	}
}

// mainStepIndices are the top-level cleaning steps in display order. Step 12
// (Deep Cleanup) is included for label/width purposes even though its toggle
// lives outside Config's GetVal/SetVal.
var mainStepIndices = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

// ramSubOptions are the config indices of the RAM Optimization sub-toggles,
// in display order.
var ramSubOptions = []int{101, 102, 103, 104}

// stepLabel builds the label for a top-level step in two forms: the plain
// text, used for width and padding math, and the colored text actually
// printed. They differ only for Deep Cleanup, whose "RUN ONCE" tag is red -
// ANSI escapes have zero visible width but do count toward len(), so padding
// must always be measured on the plain form or the column drifts.
func stepLabel(index int) (plain, colored string) {
	num := fmt.Sprintf("[%d]", index)
	name := T(fmt.Sprintf("step.%d.name", index))

	if index == 12 {
		tag := T("step.12.tag")
		plain = fmt.Sprintf("%-5s%s - %s", num, name, tag)
		colored = fmt.Sprintf("%-5s%s - %s%s%s", num, name, RED, tag, RST)
		return plain, colored
	}

	plain = fmt.Sprintf("%-5s%s", num, name)
	return plain, plain
}

// subLabel builds an indented RAM sub-option label: "  - <name> (<note>)".
// The name is padded to the widest name in the group so every "(" lines up,
// whatever the language.
func subLabel(index int) string {
	w := maxKeyWidth(
		"sub.101.name", "sub.102.name", "sub.103.name", "sub.104.name",
	)
	name := padRight(T(fmt.Sprintf("sub.%d.name", index)), w)
	return fmt.Sprintf("  - %s (%s)", name, T(fmt.Sprintf("sub.%d.note", index)))
}

// mainLabelWidth and subLabelWidth give each group its own ON/OFF column,
// matching the original layout where the indented sub-options sit further
// right than the numbered steps. Widths are measured, not typed, so a longer
// translation shifts the column instead of breaking the alignment.
func mainLabelWidth() int {
	w := 0
	for _, i := range mainStepIndices {
		plain, _ := stepLabel(i)
		w = max(w, len([]rune(plain)))
	}
	return w
}

func subLabelWidth() int {
	w := 0
	for _, i := range ramSubOptions {
		w = max(w, len([]rune(subLabel(i))))
	}
	return w
}

// GetCfgDispName returns the display label for a config index, already
// padded so the ON/OFF column that follows lines up with its group.
func GetCfgDispName(index int) string {
	if index >= 101 {
		return padRight(subLabel(index), subLabelWidth())
	}
	if index < 1 || index > 12 {
		return ""
	}
	plain, colored := stepLabel(index)
	if pad := mainLabelWidth() - len([]rune(plain)); pad > 0 {
		colored += strings.Repeat(" ", pad)
	}
	return colored
}

// StepHeader prints the "[n/12] <action>..." banner shown while a step runs.
// Deep Cleanup (12) is printed in red, as it always was, because it is the
// one destructive step.
func StepHeader(idx int) {
	head := fmt.Sprintf("[%d/12] %s", idx, T(fmt.Sprintf("step.%d.action", idx)))
	if idx == 12 {
		head = RED + head + RST
	}
	TypeLine(head, 0)
}

// StepSkipped prints the one-line "[n/12] <name> - SKIPPED" notice for a step
// that is switched off. The label is padded to the widest "[n/12] <name>" in
// the current language so every dash lines up, which is what the typed
// trailing spaces in the original English strings were doing by hand.
func StepSkipped(idx int) {
	head := func(i int) string {
		return fmt.Sprintf("[%d/12] %s", i, T(fmt.Sprintf("step.%d.name", i)))
	}
	w := 0
	for i := 1; i <= 12; i++ {
		w = max(w, len([]rune(head(i))))
	}
	fmt.Printf("%s - %s%s%s\n", padRight(head(idx), w), CYAN, T("common.skipped"), RST)
}

// selectedMark is the tick shown beside the active language. The console is
// switched to UTF-8 at startup, so this renders on Windows Terminal and
// modern conhost; it is two columns wide to match the blank alternative.
const selectedMark = "✓ "

// boxWidth is the visible width of the "+---...---+" rules used as screen
// headers, including both "+" characters.
const boxWidth = 41

// centerInBox pads a heading with leading spaces so it sits centered under
// the box rule. Headings used to carry hand-typed indentation, which only
// looked centered for the exact English wording.
func centerInBox(s string) string {
	n := len([]rune(s))
	if n >= boxWidth {
		return s
	}
	return strings.Repeat(" ", (boxWidth-n)/2) + s
}

// RenderToggle formats a step's state as a colored [ON] / [OFF] marker.
func RenderToggle(on bool) string {
	if on {
		return fmt.Sprintf("%s[%s]%s", RED, T("common.on"), RST)
	}
	return fmt.Sprintf("%s[%s]%s", CYAN, T("common.off"), RST)
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
	// Reserve the stats row but leave it EMPTY - StartLiveStatsRow paints it
	// immediately and then once a second. Printing a static line here too
	// would mean two stats lines on screen (a stale one and a live one).
	// It sits above the box so both screens show stats in the same place.
	statsRowNum := row + 1
	pln()
	pln("+---------------------------------------+")
	pln("            QUIESCE")
	pln("+---------------------------------------+")
	pf("  %s%s ~ %s%s v%s%s\n", WHITE, Author, RED, AppName, Version, RST)
	pf("  %s%s%s%s%s\n", WHITE, osP1, RED, osP2, RST)
	pln("+---------------------------------------+")
	pln()
	pf("%s%s%s\n", WHITE, T("menu.will_clean"), RST)

	for i := 1; i <= 11; i++ {
		val := cfg.GetVal(i)
		pf("  %s : %s\n", GetCfgDispName(i), RenderToggle(val == 1))

		// Show which RAM operations will actually run, indented under [10].
		if i == 10 && val == 1 {
			for _, sub := range ramSubOptions {
				pf("  %s : %s\n", GetCfgDispName(sub), RenderToggle(cfg.GetVal(sub) == 1))
			}
		}
	}

	// Deep Cleanup — inline, right below Recycle Bin
	pf("  %s : %s\n", GetCfgDispName(12), RenderToggle(cfg.DeepCleanup == 1))

	pln()
	pln("+---------------------------------------+")
	pf("  %s\n", Tf("menu.hint", WHITE, RST, WHITE, RST))
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
	// Navigable rows, in display order: steps 1-10, the four RAM
	// sub-options indented under [10], then step 11 and Deep Cleanup (12).
	entries := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	entries = append(entries, ramSubOptions...)
	entries = append(entries, 11, 12)

	// sel is a position in entries, not a config index.
	sel := 0

	for {
		// Row counter, same technique as DrawMainMenu: the prompt row is
		// computed as the screen is drawn rather than hand-counted, so the
		// live stats painter always parks the cursor in the right place.
		row := 0
		pln := func(a ...interface{}) {
			fmt.Println(a...)
			row++
		}
		pf := func(format string, a ...interface{}) {
			fmt.Printf(format, a...)
			row++
		}

		ClearScreen()
		pln()
		// Reserved (empty) stats row - painted live, above the box, exactly
		// as on the main menu.
		statsRowNum := row + 1
		pln()
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("%s%s%s\n", WHITE, centerInBox(T("settings.title")), RST)
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("  %s\n", Tf("settings.keys", WHITE, RST, WHITE, RST, WHITE, RST, WHITE, RST, WHITE, RST, WHITE, RST))
		pln()

		if statsRowNum != liveStatsRow {
			pf("\x1b[33m[WARN]\x1b[0m liveStatsRow constant (%d) doesn't match actual row (%d). Update liveStatsRow in ui.go.\n", liveStatsRow, statsRowNum)
		}

		for pos, idx := range entries {
			row++
			a := "   "
			if sel == pos {
				a = fmt.Sprintf(" %s<<<<<%s", YELLOW, RST)
			}

			// Deep Cleanup (12) isn't part of Config's GetVal/SetVal - it is
			// never persisted, so its state is read separately. Its label
			// still comes from GetCfgDispName so it shares the same measured
			// column as every other step.
			on := cfg.GetVal(idx) == 1
			if idx == 12 {
				on = cfg.DeepCleanup == 1
			}
			fmt.Printf("   %s : %s%s\n", GetCfgDispName(idx), RenderToggle(on), a)
		}

		pln()
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("  %s\n", Tf("settings.save_hint", WHITE, RST))
		pf("%s+---------------------------------------+%s\n", WHITE, RST)

		// Prompt first, then the painter - the painter parks the cursor at
		// the prompt's column, so the prompt has to already be on screen.
		fmt.Print("  > ")
		statsStop := StartLiveStatsRow(row + 1)

		ch := ReadSingleKey()

		// Stop (and WAIT for) the painter before this screen is redrawn or
		// replaced, so it can never write over the next screen.
		StopLiveStatsRow(statsStop)

		chStr := strings.ToLower(string(ch))

		switch chStr {
		case "w":
			sel--
			if sel < 0 {
				sel = len(entries) - 1
			}
		case "s":
			sel++
			if sel > len(entries)-1 {
				sel = 0
			}
		case "a":
			if entries[sel] == 12 {
				cfg.DeepCleanup = 0
			} else {
				cfg.SetVal(entries[sel], 0)
			}
		case "d":
			if entries[sel] == 12 {
				cfg.DeepCleanup = 1
			} else {
				cfg.SetVal(entries[sel], 1)
			}
		case "l":
			// The picker redraws this screen in the new language on return,
			// because the loop repaints from scratch every iteration.
			RunLanguageScreen(cfg, configFilePath)
		case "e":
			// Save config — DeepCleanup is intentionally NOT saved
			_ = SaveConfig(configFilePath, cfg)
			fmt.Println()
			fmt.Printf("  %s[SAVED]%s %s\n", WHITE, RST, T("settings.saved"))
			time.Sleep(1 * time.Second)
			ClearScreen()
			return
		}
	}
}

// RunLanguageScreen shows the language picker reached with L from the
// settings screen. The user presses the number next to a language to switch
// to it, and E to save and go back.
//
// The switch is applied immediately rather than on save, so the list itself
// redraws in the chosen language - that is the fastest way for someone who
// picked the wrong entry to realise it and pick again.
func RunLanguageScreen(cfg *Config, configFilePath string) {
	codes := AvailableLanguages()

	for {
		row := 0
		pln := func(a ...interface{}) {
			fmt.Println(a...)
			row++
		}
		pf := func(format string, a ...interface{}) {
			fmt.Printf(format, a...)
			row++
		}

		ClearScreen()
		pln()
		// Reserved (empty) stats row, painted live - same position as the
		// main menu and the settings screen.
		statsRowNum := row + 1
		pln()
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("%s%s%s\n", WHITE, centerInBox(T("language.title")), RST)
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("  %s\n", Tf("language.keys", WHITE, RST, WHITE, RST))
		pln()

		if statsRowNum != liveStatsRow {
			pf("\x1b[33m[WARN]\x1b[0m liveStatsRow constant (%d) doesn't match actual row (%d). Update liveStatsRow in ui.go.\n", liveStatsRow, statsRowNum)
		}

		// Names are padded to the widest so the tick column lines up.
		nameW := 0
		for _, c := range codes {
			nameW = max(nameW, len([]rune(LanguageDisplayName(c))))
		}

		for i, c := range codes {
			name := padRight(LanguageDisplayName(c), nameW)
			mark := "  "
			if c == ActiveLanguage() {
				mark = selectedMark
			}
			pf("   [%d] %s   %s%s%s\n", i+1, name, RED, mark, RST)
		}

		pln()
		pf("%s+---------------------------------------+%s\n", WHITE, RST)
		pf("  %s\n", Tf("language.save_hint", WHITE, RST))
		pf("%s+---------------------------------------+%s\n", WHITE, RST)

		// Prompt first, then the painter - the painter parks the cursor at
		// the prompt's column, so the prompt has to already be on screen.
		fmt.Print("  > ")
		statsStop := StartLiveStatsRow(row + 1)

		ch := ReadSingleKey()

		StopLiveStatsRow(statsStop)

		// Digits select a language by position. Anything out of range is
		// ignored rather than treated as an error.
		if ch >= '1' && ch <= '9' {
			if idx := int(ch - '1'); idx < len(codes) {
				SetLanguage(codes[idx])
				cfg.Language = codes[idx]
			}
			continue
		}

		if strings.ToLower(string(ch)) == "e" {
			// Saving here as well as on the settings screen means a language
			// picked and confirmed with E is never lost, whichever way the
			// user leaves the settings screen afterwards.
			_ = SaveConfig(configFilePath, cfg)
			fmt.Println()
			fmt.Printf("  %s[SAVED]%s %s\n", WHITE, RST, T("settings.saved"))
			time.Sleep(1 * time.Second)
			ClearScreen()
			return
		}
	}
}

func PrintSummary(cfg *Config, results StepResults) {
	fmt.Println()
	fmt.Println("+---------------------------------------+")
	fmt.Println(centerInBox(T("summary.title")))
	fmt.Println("+---------------------------------------+")
	fmt.Println()
	fmt.Printf("  %s%s%s\n", RED, Tf("summary.total", results.TotalDeleted), RST)
	fmt.Printf("  %s\n", Tf("summary.skipped", results.TotalFailed))
	fmt.Println()

	// Summary labels are padded to the widest translated label rather than
	// carrying typed trailing spaces, so the ":" column holds in any language.
	// Deep Cleanup's row carries its "RUN ONCE" tag inline, making it the
	// longest label, so it takes part in the width measurement too - it used
	// to be printed with hand-typed spacing and sat one column off even in
	// English.
	deepPlain := fmt.Sprintf("%s - %s", T("step.12.short"), T("step.12.tag"))

	shortKeys := make([]string, 0, 11)
	for i := 1; i <= 11; i++ {
		shortKeys = append(shortKeys, fmt.Sprintf("step.%d.short", i))
	}
	nameW := max(maxKeyWidth(shortKeys...), len([]rune(deepPlain)))

	// line prints one summary row: "  [n] <label> : <value>".
	line := func(idx int, value string) {
		name := padRight(T(fmt.Sprintf("step.%d.short", idx)), nameW)
		fmt.Printf("  %-5s%s : %s\n", fmt.Sprintf("[%d]", idx), name, value)
	}
	dim := func(s string) string { return CYAN + s + RST }

	steps := []struct {
		idx  int
		cfg  int
		isSp bool
	}{
		{1, cfg.WinTemp, false},
		{2, cfg.UserTemp, false},
		{3, cfg.Prefetch, false},
		{4, cfg.ErrorReports, false},
		{5, cfg.DeliveryOpt, false},
		{6, cfg.WinUpdate, false},
		{7, cfg.LogFiles, false},
		{8, cfg.InstallerTemp, false},
		{9, cfg.DnsFlush, true},
		{10, cfg.RamOptimize, true},
		{11, cfg.RecycleBin, true},
	}

	for _, s := range steps {
		if s.cfg != 1 {
			line(s.idx, dim(T("common.skipped")))
			continue
		}

		switch {
		case !s.isSp:
			line(s.idx, Tf("summary.items", results.StepCounts[s.idx]))

		case s.idx == 9:
			line(s.idx, T("summary.flushed"))

		case s.idx == 10:
			r := results.Ram
			switch {
			case r.NothingToRun:
				line(s.idx, dim(T("summary.subs_off")))
			case r.PrivFailed:
				line(s.idx, dim(T("summary.no_priv")))
			case r.FreedMB > 0:
				line(s.idx, Tf("summary.ram_freed", r.FreedMB, r.BeforePct, r.AfterPct, r.DropPct))
			case r.FreedMB == 0:
				line(s.idx, T("summary.ram_zero"))
			default:
				line(s.idx, dim(Tf("summary.ram_neg", r.FreedMB)))
			}

			// Attribute the number: which of the four operations ran, and
			// how many processes the trim actually reached.
			if !r.NothingToRun && !r.PrivFailed && len(r.OpsRun) > 0 {
				detail := strings.Join(r.OpsRun, ", ")
				if r.Trimmed > 0 || r.TrimSkipped > 0 {
					detail = Tf("summary.procs", detail, r.Trimmed, r.TrimSkipped)
				}
				fmt.Printf("       %s%s%s\n", CYAN, Tf("summary.ran", detail), RST)
			}

		case s.idx == 11:
			switch {
			case results.RecycleBinFailed:
				line(s.idx, dim(T("summary.failed")))
			case results.RecycleBinBytes > 0:
				line(s.idx, Tf("summary.bin_freed", FormatBytesHuman(results.RecycleBinBytes)))
			default:
				line(s.idx, T("summary.bin_empty"))
			}
		}
	}

	// Deep Cleanup summary line. Its label carries the red "RUN ONCE" tag, so
	// it is built here rather than through line() - padding is applied from
	// the plain form, since the color escapes have no visible width.
	deepTag := fmt.Sprintf("%s - %s%s%s%s", T("step.12.short"), RED, T("step.12.tag"), RST,
		strings.Repeat(" ", max(0, nameW-len([]rune(deepPlain)))))
	deepState := dim(T("common.skipped"))
	if results.DeepCleanupRan {
		if results.DeepCleanupFailed {
			deepState = dim(T("summary.partial"))
		} else {
			deepState = RED + T("summary.completed") + RST
		}
	}
	fmt.Printf("  %-5s%s : %s\n", "[12]", deepTag, deepState)

	fmt.Println()
	fmt.Println("+---------------------------------------+")
	fmt.Printf("  %s\n", T("summary.all_done"))
	fmt.Println("+---------------------------------------+")
	fmt.Println()
	fmt.Printf("  %s\n", VersionLine())
	fmt.Printf("  %s%s%s\n", CYAN, Repo, RST)
	fmt.Println()
}
