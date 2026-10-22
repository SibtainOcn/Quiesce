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

// LiveView shows progress while a list of tasks runs, in one of two styles:
//
// Verbose (default) - the classic output: every step banner and "[OK] file"
// line scrolls as it always has. The bottom three rows of the window are
// held back (a console scroll region) for a status line that stays put:
//
//	[OK] CHROME.EXE-58E8CA.pf
//	[OK] ~DF8A528D.TMP
//	────────────────────────────────────────────────────────────
//	⠋ [1/12] Windows Temp   ━━━━──────────   7% │ 97 files │ 43.5 MB │ 0.9s
//	CPU 12%  ·  MEMORY 6.21/15.80 GB 39%           [P] pause  [Q] abort
//
// Minimal - no file names, just a checklist that ticks over:
//
//	Cleaning  1 of 10 steps
//
//	✓  Windows Temp        140 files    1.3s
//	⠹  User Temp                  96
//	·  Prefetch
//	-  Recycle Bin           skipped
//
//	━━━━──────────────  236 files · 96.6 MB · 2.1s
//	CPU 12%  ·  MEMORY 6.21/15.80 GB 39%           [P] pause  [Q] abort
//
// The view knows nothing about cleaning, so other features can reuse it by
// handing RunTasks a different task list. Where the console can't draw it
// (no VT support, output redirected, tiny window), output is plain lines.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procKbhit                   = modmsvcrt.NewProc("_kbhit")
	procGetConsoleWindow        = modkernel32Title.NewProc("GetConsoleWindow")
	procGetCurrentConsoleFontEx = modkernel32Title.NewProc("GetCurrentConsoleFontEx")
	procGetClassNameW           = moduser32.NewProc("GetClassNameW")
)

// forcePlainView keeps the view off the console: no drawing, no keyboard
// reading. Tests set it so a `go test` run in a terminal is left alone.
var forcePlainView bool

// activeView is the view currently running, if any. The shutdown handler
// uses it to abort on Ctrl+C and to tidy up when the window is closed.
var activeView atomic.Pointer[LiveView]

// OutputStyle is the user's choice of progress display.
type OutputStyle int

const (
	StyleVerbose OutputStyle = iota // scrolling file log + pinned status line
	StyleMinimal                    // checklist only
)

type viewMode uint8

const (
	modePlain viewMode = iota
	modeVerbose
	modeMinimal
)

// LogKind picks how a log line is colored.
type LogKind uint8

const (
	LogPlain       LogKind = iota
	LogHeader              // task banner, white
	LogHeaderAlert         // banner for a destructive task, red
	LogOK                  // "[OK] text"
	LogWarn                // "[WARN] text"
	LogError               // "[ERROR] text"
	LogInfo                // cyan text
	LogGood                // red text - a success detail, in the app's accent
	LogBad                 // yellow text - a non-fatal problem
	LogSkip                // "<label> - SKIPPED"
)

type taskState uint8

const (
	taskPending taskState = iota
	taskRunning
	taskDone
	taskWarn
	taskOff    // switched off in settings
	taskNotRun // enabled, but the run was aborted before it started
)

type liveTask struct {
	name      string
	state     taskState
	detail    string
	started   time.Time
	pauseBase time.Duration // v.pausedFor when the task started
	took      time.Duration // run time, excluding pauses
}

const (
	frameEvery   = 80 * time.Millisecond // redraw rate (~12 fps)
	statsEvery   = time.Second           // CPU/RAM refresh rate
	keyPollEvery = 40 * time.Millisecond

	pinnedRows = 3 // verbose: divider, status line, CPU/RAM + keys
	minCols    = 50

	detailW = 10 // checklist value column, e.g. "140 files"
	timeW   = 6  // checklist time column, e.g. "1.3s"
	barW    = 20 // progress bar on the verbose status line
	minBarW = 30 // progress bar under the minimal checklist
)

// glyphSet holds the symbols drawn on screen. Windows Terminal and Cascadia
// fonts have braille and check marks; the legacy console font (Consolas)
// does not, and would draw empty boxes, so plain symbols are used there.
type glyphSet struct {
	spin  []string
	check string
	pend  string
	off   string
	warn  string
}

var (
	fancyGlyphs = glyphSet{
		spin:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		check: "✓", pend: "·", off: "-", warn: "!",
	}
	basicGlyphs = glyphSet{
		spin:  []string{"|", "/", "-", `\`},
		check: "√", pend: ".", off: "-", warn: "!",
	}
)

// pickGlyphs chooses fancy symbols for Windows Terminal (and other modern
// hosts, which run behind a pseudo console) and for a classic console set to
// a Cascadia font; plain ones otherwise.
func pickGlyphs() glyphSet {
	if os.Getenv("WT_SESSION") != "" || os.Getenv("TERM_PROGRAM") != "" {
		return fancyGlyphs
	}
	switch consoleWindowClass() {
	case "PseudoConsoleWindow":
		return fancyGlyphs
	case "ConsoleWindowClass":
		if strings.HasPrefix(consoleFontFace(), "Cascadia") {
			return fancyGlyphs
		}
	}
	return basicGlyphs
}

type LiveView struct {
	mode        viewMode
	interactive bool
	glyphs      glyphSet
	title       string // checklist heading, e.g. "Cleaning"

	mu        sync.Mutex // guards everything below, and all console writes
	tasks     []liveTask
	cur       int // index of the task last started, -1 before any
	enabled   int
	nameW     int // widest task name, for column alignment
	skipW     int // widest "[n/N] name" label, so SKIPPED lines align
	stats     SystemStats
	start     time.Time
	endAt     time.Time
	pausedAt  time.Time
	pausedFor time.Duration
	finished  bool
	lastW     int
	lastH     int
	runLog    *runLog
	logPath   string
	logErr    error

	stepTotal atomic.Int64
	stepDone  atomic.Int64
	items     atomic.Int64
	bytes     atomic.Int64

	pmu     sync.Mutex // pairs with pcond for pause/resume
	pcond   *sync.Cond
	paused  atomic.Bool
	aborted atomic.Bool

	stop      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewLiveView prepares the display for the given tasks. title heads the
// minimal checklist. enabled[i] false shows that task as skipped. logDir,
// when not empty, is where a plain-text copy of the log is written; a
// failure to open it is reported and the run carries on without it.
func NewLiveView(style OutputStyle, title string, names []string, enabled []bool, logDir string) *LiveView {
	v := &LiveView{
		title:  title,
		glyphs: basicGlyphs,
		cur:    -1,
		start:  time.Now(),
		stop:   make(chan struct{}),
	}
	v.pcond = sync.NewCond(&v.pmu)

	for i, n := range names {
		t := liveTask{name: n, state: taskOff}
		if i < len(enabled) && enabled[i] {
			t.state = taskPending
			v.enabled++
		}
		v.tasks = append(v.tasks, t)
		v.nameW = max(v.nameW, textWidth(n))
		v.skipW = max(v.skipW, textWidth(v.labelFor(i)))
	}

	if logDir != "" {
		l, err := openRunLog(logDir)
		if err != nil {
			v.logErr = err
		} else {
			v.runLog = l
			v.logPath = l.path
			l.line(Tf("log.header", AppName, Version, v.start.Format("2006-01-02 15:04:05")))
		}
	}

	if !forcePlainView {
		v.mode = pickMode(style, len(v.tasks))
		v.interactive = stdinIsConsole()
		v.glyphs = pickGlyphs()
	}

	// Anything typed before the run started (a held ENTER, say) must not be
	// read as P or Q.
	if v.interactive {
		FlushKeyBuffer()
	}

	if v.mode != modePlain {
		v.stats = GetSystemStats()
		w, h, _ := consoleSize()
		v.mu.Lock()
		var b strings.Builder
		b.WriteString("\x1b[?25l") // hide the cursor while drawing
		b.WriteString("\x1b[H\x1b[2J")
		if v.mode == modeVerbose {
			// Logs scroll in rows 1..h-3; the rest is ours.
			fmt.Fprintf(&b, "\x1b[1;%dr\x1b[1;1H", h-pinnedRows)
		}
		v.lastW, v.lastH = w, h
		v.drawLocked(&b, w, h, time.Now())
		_, _ = os.Stdout.WriteString(b.String())
		v.mu.Unlock()

		v.wg.Add(2)
		go v.renderLoop()
		go v.statsLoop()
	}
	if v.interactive {
		v.wg.Add(1)
		go v.keyLoop()
	}

	activeView.Store(v)
	if v.logErr != nil {
		v.Log(LogWarn, Tf("summary.log_fail", v.logErr))
	}
	return v
}

// pickMode falls back to plain output when the console can't hold the
// chosen style.
func pickMode(style OutputStyle, tasks int) viewMode {
	w, h, ok := consoleSize()
	if !ok || !stdoutSupportsVT() || w < minCols {
		return modePlain
	}
	if style == StyleMinimal && h > minimalRows(tasks) {
		return modeMinimal
	}
	if h >= pinnedRows+5 {
		return modeVerbose
	}
	return modePlain
}

// minimalRows is the height of the minimal checklist block.
func minimalRows(tasks int) int { return tasks + 6 }

func (v *LiveView) labelFor(i int) string {
	return fmt.Sprintf("[%d/%d] %s", i+1, len(v.tasks), v.tasks[i].name)
}

// --- Called by the task runner ------------------------------------------

// BeginTask marks task i as running and prints its banner.
func (v *LiveView) BeginTask(i int, title string, alert bool) {
	v.stepTotal.Store(0)
	v.stepDone.Store(0)
	v.mu.Lock()
	v.cur = i
	v.tasks[i].state = taskRunning
	v.tasks[i].started = time.Now()
	v.tasks[i].pauseBase = v.pausedFor
	v.mu.Unlock()

	v.Log(LogPlain, "")
	kind := LogHeader
	if alert {
		kind = LogHeaderAlert
	}
	v.Log(kind, title)
}

// EndTask records how task i finished and the value shown beside it.
func (v *LiveView) EndTask(i int, detail string, warn bool) {
	now := time.Now()
	v.mu.Lock()
	t := &v.tasks[i]
	paused := v.pausedFor - t.pauseBase
	if v.paused.Load() {
		paused += now.Sub(v.pausedAt)
	}
	t.took = max(0, now.Sub(t.started)-paused)
	t.detail = detail
	t.state = taskDone
	if warn {
		t.state = taskWarn
	}
	v.mu.Unlock()
	v.Log(LogPlain, T("common.done"))
}

// SkipTask prints the "SKIPPED" line for a task that is switched off.
func (v *LiveView) SkipTask(i int) {
	v.Log(LogPlain, "")
	v.Log(LogSkip, padRight(v.labelFor(i), v.skipW))
}

// NotRun marks an enabled task that never started because of an abort.
func (v *LiveView) NotRun(i int) {
	v.mu.Lock()
	v.tasks[i].state = taskNotRun
	v.mu.Unlock()
}

// SetTotal tells the view how many items the running task will process, so
// the progress bar can move within the task.
func (v *LiveView) SetTotal(n int) { v.stepTotal.Store(int64(n)) }

// Deleted counts one removed item and logs it as "[OK] name".
func (v *LiveView) Deleted(name string, size int64) {
	v.items.Add(1)
	if size > 0 {
		v.bytes.Add(size)
	}
	v.stepDone.Add(1)
	v.Log(LogOK, name)
}

// Failed counts an item that could not be removed (in use, protected).
func (v *LiveView) Failed() { v.stepDone.Add(1) }

// AddBytes adds space freed without a whole item being removed, such as a
// folder that was only partly deleted.
func (v *LiveView) AddBytes(n int64) {
	if n > 0 {
		v.bytes.Add(n)
	}
}

// Log writes a line to the log file and, except in minimal style, to the
// screen. Verbose prints it like the classic output; the console's scroll
// region keeps it above the pinned status rows.
func (v *LiveView) Log(kind LogKind, text string) {
	line := renderLogLine(kind, text)
	v.mu.Lock()
	defer v.mu.Unlock()

	v.runLog.line(stripANSI(line))
	if v.mode == modeMinimal || v.finished && v.mode != modePlain {
		return
	}
	var b strings.Builder
	b.WriteString(line)
	b.WriteString("\n")
	if kind == LogHeader || kind == LogHeaderAlert {
		b.WriteString("+---------------------------------------+\n")
	}
	_, _ = os.Stdout.WriteString(b.String())
}

// Checkpoint is called between items. It blocks while the run is paused and
// returns false once the run has been aborted, so the caller stops.
func (v *LiveView) Checkpoint() bool {
	if v.paused.Load() {
		v.pmu.Lock()
		for v.paused.Load() && !v.aborted.Load() {
			v.pcond.Wait()
		}
		v.pmu.Unlock()
	}
	return !v.aborted.Load()
}

// Aborted reports whether the run was aborted (Q or Ctrl+C).
func (v *LiveView) Aborted() bool { return v.aborted.Load() }

func (v *LiveView) isFinished() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.finished
}

// TogglePause pauses or resumes the run. Work in progress finishes its
// current item first; nothing is interrupted halfway.
func (v *LiveView) TogglePause() {
	v.pmu.Lock()
	if v.aborted.Load() {
		v.pmu.Unlock()
		return
	}
	now := time.Now()
	pausing := !v.paused.Load()
	v.mu.Lock()
	if v.finished {
		v.mu.Unlock()
		v.pmu.Unlock()
		return
	}
	if pausing {
		v.pausedAt = now
	} else {
		v.pausedFor += now.Sub(v.pausedAt)
	}
	v.mu.Unlock()
	v.paused.Store(pausing)
	v.pcond.Broadcast()
	v.pmu.Unlock()

	if pausing {
		v.Log(LogBad, T("live.paused_log"))
	} else {
		v.Log(LogInfo, T("live.resumed"))
	}
}

// Abort stops the run at the next checkpoint. Services stopped by a task are
// still restarted, because tasks restart them with defer.
func (v *LiveView) Abort() {
	if v.isFinished() || v.aborted.Swap(true) {
		return
	}
	v.pmu.Lock()
	if v.paused.Load() {
		v.mu.Lock()
		v.pausedFor += time.Since(v.pausedAt)
		v.mu.Unlock()
		v.paused.Store(false)
	}
	v.pcond.Broadcast()
	v.pmu.Unlock()
	v.Log(LogWarn, T("live.abort_req"))
}

// Totals returns items removed, bytes freed and running time so far.
func (v *LiveView) Totals() (items, bytes int64, elapsed time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.items.Load(), v.bytes.Load(), v.elapsedLocked(time.Now())
}

// Finish stops the clock and makes the status show [DONE] or [ABORTED].
func (v *LiveView) Finish() {
	now := time.Now()
	v.mu.Lock()
	if v.finished {
		v.mu.Unlock()
		return
	}
	if v.paused.Load() {
		v.pausedFor += now.Sub(v.pausedAt)
	}
	v.finished = true
	v.endAt = now
	v.mu.Unlock()

	v.pmu.Lock()
	v.paused.Store(false)
	v.pcond.Broadcast()
	v.pmu.Unlock()
}

// Close stops the background goroutines and hands the console back in its
// normal state, with the cursor where the next output (the summary) should
// go. The log file is closed. Safe to call more than once.
func (v *LiveView) Close() (logPath string, logErr error) {
	v.closeOnce.Do(func() {
		v.Finish()
		close(v.stop)
		v.wg.Wait()

		v.mu.Lock()
		var b strings.Builder
		w, h, ok := consoleSize()
		if !ok {
			w, h = v.lastW, v.lastH
		}
		switch v.mode {
		case modeVerbose:
			// Clear the pinned rows, give the whole window back to normal
			// scrolling, then print the final status where the log ended -
			// the summary follows it like any other output.
			b.WriteString("\x1b7")
			for r := h - pinnedRows + 1; r <= h; r++ {
				fmt.Fprintf(&b, "\x1b[%d;1H\x1b[2K", r)
			}
			b.WriteString("\x1b[r\x1b8")
			b.WriteString("\n" + clipANSI(v.statusLocked(w-1, time.Now()), w-1) + "\n")
		case modeMinimal:
			v.drawLocked(&b, w, h, time.Now())
			fmt.Fprintf(&b, "\x1b[%d;1H", minimalRows(len(v.tasks))+1)
		}
		if v.mode != modePlain {
			b.WriteString("\x1b[?25h")
			_, _ = os.Stdout.WriteString(b.String())
		}
		v.closeLogLocked()
		v.mu.Unlock()
		activeView.CompareAndSwap(v, nil)
	})
	return v.logPath, v.logErr
}

// emergencyClose runs when the process is about to be killed (window
// closed, logoff): it saves the log without waiting for anything.
func (v *LiveView) emergencyClose() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.runLog.line(T("live.closed_log"))
	v.closeLogLocked()
}

func (v *LiveView) closeLogLocked() {
	if v.runLog == nil {
		return
	}
	v.runLog.line("")
	v.runLog.line(Tf("log.footer", formatElapsed(v.elapsedLocked(time.Now())),
		v.items.Load(), FormatBytesHuman(uint64(v.bytes.Load()))))
	if _, err := v.runLog.close(); err != nil {
		v.logErr = err
	}
	v.runLog = nil
}

// --- Background goroutines ------------------------------------------------

func (v *LiveView) renderLoop() {
	defer v.wg.Done()
	// A drawing bug must never take the cleaning run down with it.
	defer func() { _ = recover() }()

	t := time.NewTicker(frameEvery)
	defer t.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-t.C:
			v.render()
		}
	}
}

func (v *LiveView) statsLoop() {
	defer v.wg.Done()
	defer func() { _ = recover() }()

	t := time.NewTicker(statsEvery)
	defer t.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-t.C:
			// Sampling CPU takes ~100 ms, so it happens here and never on
			// the render path.
			s := GetSystemStats()
			v.mu.Lock()
			v.stats = s
			v.mu.Unlock()
		}
	}
}

// keyLoop polls the keyboard without blocking, so it stops as soon as the
// run ends and never swallows a key meant for the next prompt.
func (v *LiveView) keyLoop() {
	defer v.wg.Done()
	defer func() { _ = recover() }()
	if procKbhit.Find() != nil || procGetch.Find() != nil {
		return
	}

	t := time.NewTicker(keyPollEvery)
	defer t.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-t.C:
		}
		for {
			if r, _, _ := procKbhit.Call(); r == 0 {
				break
			}
			ch := ReadSingleKey()
			// Arrow and function keys arrive as two codes: a 0 or 0xE0
			// prefix, then a scan code. Down-arrow's scan code is 'P', so
			// the second code must be consumed, not read as P.
			if ch == 0 || ch == 0xE0 {
				ReadSingleKey()
				continue
			}
			switch ch {
			case 'p', 'P':
				v.TogglePause()
			case 'q', 'Q':
				v.Abort()
			}
		}
	}
}

// --- Drawing ---------------------------------------------------------------

func (v *LiveView) render() {
	w, h, ok := consoleSize()
	if !ok || w < minCols {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.finished {
		return // Close draws the final state
	}
	var b strings.Builder
	if w != v.lastW || h != v.lastH {
		switch v.mode {
		case modeVerbose:
			// Re-fit the scroll region to the new height and carry on
			// logging at its bottom.
			fmt.Fprintf(&b, "\x1b[1;%dr\x1b[%d;1H", h-pinnedRows, h-pinnedRows)
		case modeMinimal:
			b.WriteString("\x1b[H\x1b[2J")
		}
		v.lastW, v.lastH = w, h
	}
	v.drawLocked(&b, w, h, time.Now())
	_, _ = os.Stdout.WriteString(b.String())
}

// drawLocked appends the pinned rows (verbose) or the checklist block
// (minimal). Rows are positioned explicitly and kept to w-1 columns:
// writing into the last column makes some consoles wrap and scroll.
func (v *LiveView) drawLocked(b *strings.Builder, w, h int, now time.Time) {
	cols := w - 1
	switch v.mode {
	case modeVerbose:
		top := h - pinnedRows + 1
		b.WriteString("\x1b7") // save the log's cursor position
		fmt.Fprintf(b, "\x1b[%d;1H%s%s%s\x1b[K", top, DIM, strings.Repeat("─", cols), RST)
		fmt.Fprintf(b, "\x1b[%d;1H%s\x1b[K", top+1, clipANSI(v.statusLocked(cols, now), cols))
		fmt.Fprintf(b, "\x1b[%d;1H%s\x1b[K", top+2, clipANSI(v.footerLocked(cols), cols))
		b.WriteString("\x1b8")
	case modeMinimal:
		for i, row := range v.checklistLocked(cols, now) {
			fmt.Fprintf(b, "\x1b[%d;1H%s\x1b[K", i+1, clipANSI(row, cols))
		}
	}
}

// checklistLocked builds the minimal style's rows.
func (v *LiveView) checklistLocked(cols int, now time.Time) []string {
	rows := make([]string, 0, minimalRows(len(v.tasks)))
	head := "  "
	if v.title != "" {
		head += WHITE + v.title + RST + "  "
	}
	rows = append(rows, "", head+DIM+Tf("live.steps", v.startedEnabledLocked(), v.enabled)+RST, "")
	for i := range v.tasks {
		rows = append(rows, v.taskRowLocked(i, now))
	}

	p := v.progressLocked()
	filled := int(p*minBarW + 0.5)
	bar := RED + strings.Repeat("━", filled) + DIM + strings.Repeat("─", minBarW-filled) + RST
	dot := DIM + " · " + RST
	line := "  " + bar + "  " + Tf("live.files", v.items.Load()) + dot +
		FormatBytesHuman(uint64(v.bytes.Load())) + dot + formatElapsed(v.elapsedLocked(now))
	if tag := v.tagLocked(); tag != "" {
		line += "  " + tag
	}
	rows = append(rows, "", line, "  "+v.footerLocked(cols-2))
	return rows
}

// taskRowLocked draws one checklist row:
//
//	✓  Windows Temp        140 files    1.3s
//	⠹  User Temp                  96
//	·  Prefetch
//	-  Recycle Bin           skipped
func (v *LiveView) taskRowLocked(i int, now time.Time) string {
	t := v.tasks[i]
	g := v.glyphs
	name := padRight(t.name, v.nameW)
	took := "  " + DIM + padLeft(formatTaskTime(t.took), timeW) + RST
	switch t.state {
	case taskOff:
		return DIM + "  " + g.off + "  " + name + " " + padLeft(T("live.skipped"), detailW) + RST
	case taskNotRun:
		return DIM + "  " + g.off + "  " + name + " " + padLeft(T("common.not_run"), detailW) + RST
	case taskRunning:
		icon := RED + v.spinner(now) + RST
		if v.paused.Load() {
			icon = YELLOW + g.warn + RST
		}
		count := ""
		if v.stepTotal.Load() > 0 {
			count = fmt.Sprint(v.stepDone.Load())
		}
		return "  " + icon + "  " + WHITE + name + " " + padLeft(count, detailW) + RST
	case taskDone:
		return "  " + GREEN + g.check + RST + "  " + WHITE + name + RST + " " + RED + padLeft(t.detail, detailW) + RST + took
	case taskWarn:
		return "  " + YELLOW + g.warn + RST + "  " + name + " " + YELLOW + padLeft(t.detail, detailW) + RST + took
	default:
		return DIM + "  " + g.pend + "  " + t.name + RST
	}
}

func (v *LiveView) spinner(now time.Time) string {
	s := v.glyphs.spin
	return s[int(now.UnixMilli()/frameEvery.Milliseconds())%len(s)]
}

func (v *LiveView) startedEnabledLocked() int {
	n := 0
	for _, t := range v.tasks {
		if t.state == taskRunning || t.state == taskDone || t.state == taskWarn {
			n++
		}
	}
	return n
}

// progressLocked is the share of enabled tasks finished, counting the
// running task's own progress as a fraction.
func (v *LiveView) progressLocked() float64 {
	if v.enabled == 0 || (v.finished && !v.aborted.Load()) {
		return 1
	}
	done := 0.0
	for _, t := range v.tasks {
		switch t.state {
		case taskDone, taskWarn:
			done++
		case taskRunning:
			if total := v.stepTotal.Load(); total > 0 {
				done += min(1, float64(v.stepDone.Load())/float64(total))
			}
		}
	}
	return min(1, done/float64(v.enabled))
}

func (v *LiveView) elapsedLocked(now time.Time) time.Duration {
	end := now
	if v.finished {
		end = v.endAt
	}
	d := end.Sub(v.start) - v.pausedFor
	if !v.finished && v.paused.Load() {
		d -= now.Sub(v.pausedAt)
	}
	return max(0, d)
}

// tagLocked is the bracketed state tag, or "" while running normally.
func (v *LiveView) tagLocked() string {
	aborted := v.aborted.Load()
	switch {
	case v.finished && aborted:
		return RED + "[" + T("live.aborted") + "]" + RST
	case v.finished:
		return GREEN + "[" + T("live.done") + "]" + RST
	case aborted:
		return RED + "[" + T("live.aborting") + "]" + RST
	case v.paused.Load():
		return YELLOW + "[" + T("live.paused") + "]" + RST
	}
	return ""
}

// statusLocked is the verbose status line.
func (v *LiveView) statusLocked(cols int, now time.Time) string {
	label := ""
	if v.cur >= 0 {
		label = v.labelFor(v.cur)
	}
	head := v.tagLocked()
	switch {
	case head == "":
		head = RED + v.spinner(now) + RST + " " + WHITE + label + RST
	case !v.finished || v.aborted.Load():
		head += " " + WHITE + label + RST
	}
	// Fixed width, so the bar doesn't jump as task names change length.
	headW := v.nameW + 22
	head += strings.Repeat(" ", max(1, headW-visibleWidth(head)))

	p := v.progressLocked()
	filled := int(p*barW + 0.5)
	bar := RED + strings.Repeat("━", filled) + DIM + strings.Repeat("─", barW-filled) + RST
	pct := fmt.Sprintf(" %3d%%", int(p*100+0.5))

	sepr := " " + DIM + "│" + RST + " "
	stats := sepr + Tf("live.files", v.items.Load()) +
		sepr + FormatBytesHuman(uint64(v.bytes.Load())) +
		sepr + formatElapsed(v.elapsedLocked(now))

	line := head + bar + pct + stats
	if visibleWidth(line) > cols {
		line = head + stats // too narrow for the bar
	}
	return line
}

func (v *LiveView) footerLocked(cols int) string {
	s := v.stats
	left := DIM + fmt.Sprintf("%s %d%%  ·  %s %.2f/%.2f GB %d%%",
		T("menu.cpu"), s.CpuLoad, T("menu.memory"), s.UsedGB, s.TotalGB, s.Pct) + RST

	keys := ""
	switch {
	case v.finished:
	case v.aborted.Load():
		keys = T("live.keys_stopping")
	case v.paused.Load():
		keys = T("live.keys_paused")
	default:
		keys = T("live.keys_run")
	}
	gap := cols - visibleWidth(left) - textWidth(keys)
	if keys == "" || gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + WHITE + keys + RST
}

// renderLogLine colors a log line for the screen.
func renderLogLine(kind LogKind, text string) string {
	switch kind {
	case LogHeader:
		return WHITE + text + RST
	case LogHeaderAlert:
		return RED + text + RST
	case LogOK:
		return RED + "[OK]" + RST + " " + text
	case LogWarn:
		return CYAN + "[WARN]" + RST + " " + text
	case LogError:
		return RED + "[ERROR]" + RST + " " + text
	case LogInfo:
		return CYAN + text + RST
	case LogGood:
		return RED + text + RST
	case LogBad:
		return YELLOW + text + RST
	case LogSkip:
		return text + " - " + CYAN + T("common.skipped") + RST
	default:
		return text
	}
}

// formatElapsed shows tenths of a second for short runs, where "00:00"
// would say nothing, and mm:ss (or h:mm:ss) beyond a minute.
func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// formatTaskTime is the checklist's per-task time: "1.4s", or "2m05s" for
// a minute and over.
func formatTaskTime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

// --- Log file ----------------------------------------------------------------

type runLog struct {
	f    *os.File
	w    *bufio.Writer
	path string
	err  error // first write error; later writes are skipped
}

func openRunLog(dir string) (*runLog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := "quiesce_" + time.Now().Format("2006-01-02_150405") + ".log"
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	return &runLog{f: f, w: bufio.NewWriterSize(f, 64<<10), path: f.Name()}, nil
}

// line writes one line. Nil-safe, so callers needn't check whether logging
// is switched on.
func (l *runLog) line(s string) {
	if l == nil || l.err != nil {
		return
	}
	_, l.err = l.w.WriteString(s + "\r\n")
}

func (l *runLog) close() (string, error) {
	if l == nil {
		return "", nil
	}
	return l.path, errors.Join(l.err, l.w.Flush(), l.f.Close())
}

// --- Console helpers ---------------------------------------------------------

func consoleSize() (w, h int, ok bool) {
	hnd, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, 0, false
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(hnd, &info); err != nil {
		return 0, 0, false
	}
	return int(info.Window.Right-info.Window.Left) + 1, int(info.Window.Bottom-info.Window.Top) + 1, true
}

func stdoutSupportsVT() bool {
	hnd, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if err := windows.GetConsoleMode(hnd, &mode); err != nil {
		return false
	}
	return mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0
}

func stdinIsConsole() bool {
	hnd, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(hnd, &mode) == nil
}

// consoleWindowClass tells the hosts apart: "ConsoleWindowClass" is the
// classic console (conhost); "PseudoConsoleWindow" means a modern terminal
// such as Windows Terminal draws the window.
func consoleWindowClass() string {
	if procGetConsoleWindow.Find() != nil || procGetClassNameW.Find() != nil {
		return ""
	}
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// consoleFontFace returns the classic console's font name.
func consoleFontFace() string {
	if procGetCurrentConsoleFontEx.Find() != nil {
		return ""
	}
	hnd, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return ""
	}
	var f struct {
		size   uint32
		index  uint32
		fontW  int16
		fontH  int16
		family uint32
		weight uint32
		face   [32]uint16
	}
	f.size = uint32(unsafe.Sizeof(f))
	if r, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(hnd), 0, uintptr(unsafe.Pointer(&f))); r == 0 {
		return ""
	}
	return windows.UTF16ToString(f.face[:])
}

// --- Text width helpers --------------------------------------------------------
//
// Widths are counted in terminal columns: escape sequences take none, and
// CJK/emoji take two. Getting this wrong makes a pinned row wrap, which
// scrolls the screen.

func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// textWidth is the column width of s, which must contain no escapes.
func textWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// clipANSI cuts s to at most maxW visible columns, keeping its escape
// sequences intact and resetting colors if anything was cut.
func clipANSI(s string, maxW int) string {
	var b strings.Builder
	w, esc, clipped := 0, 0, false // esc: 0 text, 1 after ESC, 2 inside CSI
	for _, r := range s {
		switch {
		case esc == 1:
			b.WriteRune(r)
			esc = 2
			if r != '[' {
				esc = 0
			}
			continue
		case esc == 2:
			b.WriteRune(r)
			if r >= 0x40 && r <= 0x7E {
				esc = 0
			}
			continue
		case r == 0x1b:
			esc = 1
			b.WriteRune(r)
			continue
		}
		if clipped {
			continue
		}
		rw := runeWidth(r)
		if w+rw > maxW {
			clipped = true
			continue
		}
		w += rw
		b.WriteRune(r)
	}
	if clipped {
		b.WriteString(RST)
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := 0
	for _, r := range s {
		switch {
		case esc == 1:
			esc = 2
			if r != '[' {
				esc = 0
			}
		case esc == 2:
			if r >= 0x40 && r <= 0x7E {
				esc = 0
			}
		case r == 0x1b:
			esc = 1
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func visibleWidth(s string) int { return textWidth(stripANSI(s)) }

// padLeft right-aligns s in a field w columns wide.
func padLeft(s string, w int) string {
	if n := w - textWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}
