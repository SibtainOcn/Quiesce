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

// LiveView is the full-screen progress display used while a list of tasks
// runs. It knows nothing about cleaning: it shows whatever task names it is
// given, so any future feature (browser caches, telemetry scan, ...) gets the
// same screen by handing RunTasks a different task list.
//
//	+--------------------------------------+---------------------+
//	| scrolling log - or, once finished,   | task checklist      |
//	| the summary (scroll with up/down)    | (pinned)            |
//	+--------------------------------------+---------------------+
//	| spinner  [3/12] Prefetch  ━━━──  42% | items | MB | time   |  <- status
//	| CPU 12%  ·  MEMORY 6.21/15.8 GB 39%       [P] pause [Q] abort  <- footer
//
// The whole layout owns the window from start to exit: nothing is ever
// printed below it, so nothing can scroll it out of place. One goroutine
// redraws it about 12 times a second, writing only the rows that changed.
// Worker goroutines never write to the console directly.
//
// When the console can't do this (no VT support, output redirected, window
// too small) the view falls back to plain line-by-line printing - the same
// output Quiesce always had.

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
	procShowWindow              = moduser32.NewProc("ShowWindow")
)

// forcePlainView keeps the view off the console: no live screen, no
// keyboard reading. Tests set it so a `go test` run in a terminal doesn't
// clear the screen or eat keystrokes.
var forcePlainView bool

// activeView is the view currently on screen, if any. The shutdown handler
// uses it to abort a run on Ctrl+C and to flush the log when the window is
// closed.
var activeView atomic.Pointer[LiveView]

// LogKind picks how a log line is colored. The text itself is stored plain,
// so it can be clipped to the pane width and written to the log file as-is.
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

type logLine struct {
	kind LogKind
	text string
}

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
	name   string
	state  taskState
	detail string
}

const (
	logRingSize  = 512                   // lines kept for the log pane
	frameEvery   = 80 * time.Millisecond // redraw rate (~12 fps)
	statsEvery   = time.Second           // CPU/RAM refresh rate
	keyPollEvery = 40 * time.Millisecond

	// Below this size the live layout has no room to be useful.
	minLiveCols = 60
	minLiveRows = 12

	// The log pane must keep at least this many columns before the
	// checklist panel is allowed to take space from it.
	minLogCols = 40

	detailW = 10 // width of the value column in the checklist
	barW    = 20 // width of the progress bar on the status line

	// Key codes from _getch. Arrow and paging keys arrive as a 0 or 0xE0
	// prefix followed by one of the scan codes below.
	keyEnter = 13
	scanUp   = 72
	scanDown = 80
	scanPgUp = 73
	scanPgDn = 81
	scanHome = 71
	scanEnd  = 79
)

// glyphSet holds the symbols drawn on screen. Windows Terminal and Cascadia
// fonts have braille and check marks; the legacy console font (Consolas)
// does not, and would draw empty boxes, so plain symbols are used there.
type glyphSet struct {
	spin   []string
	check  string
	pend   string
	off    string
	warn   string
	paused string
}

var (
	fancyGlyphs = glyphSet{
		spin:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		check: "✓", pend: "·", off: "-", warn: "!", paused: "‖",
	}
	basicGlyphs = glyphSet{
		spin:  []string{"|", "/", "-", `\`},
		check: "√", pend: ".", off: "-", warn: "!", paused: "=",
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
	live        bool
	interactive bool
	glyphs      glyphSet

	mu        sync.Mutex // guards everything below up to the atomics
	ring      [logRingSize]logLine
	ringNext  int
	ringLen   int
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
	final     []string // summary shown in the log pane once finished
	finalTop  int      // first summary line on screen
	finalRows int      // rows the log pane had at the last draw
	finalW    int      // widest summary line
	lastW     int
	lastH     int
	prevRows  []string // rows as last written, to redraw only changes
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

	exit      chan struct{} // closed when ENTER is pressed on the summary
	exitOnce  sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewLiveView prepares the screen for the given tasks. enabled[i] false
// shows that task as skipped from the start. logDir, when not empty, is where
// a plain-text copy of the log is written; a failure to open it is reported
// in the log and the run carries on without it.
func NewLiveView(names []string, enabled []bool, logDir string) *LiveView {
	v := &LiveView{
		glyphs: basicGlyphs,
		cur:    -1,
		start:  time.Now(),
		stop:   make(chan struct{}),
		exit:   make(chan struct{}),
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
			l.line("")
		}
	}

	if !forcePlainView {
		w, h, ok := consoleSize()
		v.live = ok && stdoutSupportsVT() && w >= minLiveCols && h >= minLiveRows
		v.interactive = stdinIsConsole()
		v.glyphs = pickGlyphs()
	}

	// Anything typed before the run started (a held ENTER, say) must not be
	// read as P, Q or the final ENTER.
	if v.interactive {
		FlushKeyBuffer()
	}

	if v.live {
		v.stats = GetSystemStats()
		fmt.Print("\x1b[?25l") // hide the cursor while drawing
		ClearScreen()
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
	v.mu.Lock()
	v.tasks[i].detail = detail
	v.tasks[i].state = taskDone
	if warn {
		v.tasks[i].state = taskWarn
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

// Log adds a line to the log pane (and the log file). In plain mode it is
// printed straight away instead.
func (v *LiveView) Log(kind LogKind, text string) {
	l := logLine{kind, text}
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ring[v.ringNext] = l
	v.ringNext = (v.ringNext + 1) % logRingSize
	if v.ringLen < logRingSize {
		v.ringLen++
	}
	v.runLog.line(plainLogLine(l))

	if !v.live {
		fmt.Println(renderLogLine(l))
		if kind == LogHeader || kind == LogHeaderAlert {
			fmt.Println("+---------------------------------------+")
		}
	}
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

// Finish marks the run as over: the clock stops, the status line shows
// [DONE] or [ABORTED], and P/Q no longer do anything.
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

// ShowSummary puts the end-of-run summary on screen and writes it to the log
// file. On the live screen it replaces the log pane - the checklist and
// status lines stay where they are - and waits for ENTER, returning true.
// In plain mode it prints the lines and returns false, leaving the "press
// ENTER" prompt to the caller.
func (v *LiveView) ShowSummary(lines []string) (waitedForExit bool) {
	v.Finish()
	v.mu.Lock()
	for _, l := range lines {
		v.runLog.line(stripANSI(l))
	}
	if !v.live {
		v.mu.Unlock()
		for _, l := range lines {
			fmt.Println(l)
		}
		return false
	}
	v.final = lines
	v.finalTop = 0
	v.finalW = 0
	for _, l := range lines {
		v.finalW = max(v.finalW, visibleWidth(l))
	}
	v.mu.Unlock()

	if !v.interactive {
		return false
	}
	<-v.exit
	return true
}

// scrollSummary moves the summary by delta lines, clamped to its length.
func (v *LiveView) scrollSummary(delta int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	maxTop := max(0, len(v.final)-v.finalRows)
	v.finalTop = min(max(0, v.finalTop+delta), maxTop)
}

// Close stops the background goroutines, draws the final frame, restores
// the cursor and closes the log file. It is safe to call more than once.
func (v *LiveView) Close() (logPath string, logErr error) {
	v.closeOnce.Do(func() {
		v.Finish()
		close(v.stop)
		v.wg.Wait()

		if v.live {
			v.render()
			// Leave the cursor on the bottom row, cleared, so a shell
			// prompt that follows (when run from an existing terminal)
			// lands there instead of scrolling the screen.
			_, h, ok := consoleSize()
			if !ok {
				h = v.lastH
			}
			fmt.Printf("\x1b[%d;1H\x1b[2K\x1b[?25h", h)
		}

		v.mu.Lock()
		v.closeLogLocked()
		v.mu.Unlock()
		activeView.CompareAndSwap(v, nil)
	})
	return v.logPath, v.logErr
}

// emergencyClose is used when the process is about to be killed (window
// closed, logoff): it saves the log without waiting for anything.
func (v *LiveView) emergencyClose() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.runLog != nil {
		v.runLog.line("")
		v.runLog.line(T("live.closed_log"))
	}
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

// keyLoop polls the keyboard without blocking, so it can stop as soon as
// the view closes and never swallows a key meant for the next prompt.
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
			v.handleKey(ReadSingleKey())
		}
	}
}

func (v *LiveView) handleKey(ch rune) {
	// Arrow and paging keys arrive as two codes. The second is a scan code,
	// and down-arrow's happens to be 'P', so it must never be read as P.
	if ch == 0 || ch == 0xE0 {
		page := max(1, v.pageRows()-1)
		switch ReadSingleKey() {
		case scanUp:
			v.scrollSummary(-1)
		case scanDown:
			v.scrollSummary(1)
		case scanPgUp:
			v.scrollSummary(-page)
		case scanPgDn:
			v.scrollSummary(page)
		case scanHome:
			v.scrollSummary(-1 << 20)
		case scanEnd:
			v.scrollSummary(1 << 20)
		}
		return
	}

	if v.isFinished() {
		switch ch {
		case keyEnter:
			v.exitOnce.Do(func() { close(v.exit) })
		case 'w', 'W':
			v.scrollSummary(-1)
		case 's', 'S':
			v.scrollSummary(1)
		}
		return
	}
	switch ch {
	case 'p', 'P':
		v.TogglePause()
	case 'q', 'Q':
		v.Abort()
	}
}

func (v *LiveView) pageRows() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.finalRows
}

// --- Drawing ---------------------------------------------------------------

func (v *LiveView) render() {
	w, h, ok := consoleSize()
	if !ok || w < minLiveCols || h < minLiveRows {
		return
	}
	v.mu.Lock()
	frame := v.frameLocked(w, h, time.Now())
	v.mu.Unlock()
	if frame != "" {
		_, _ = os.Stdout.WriteString(frame)
	}
}

// frameLocked returns what must be written to bring the screen up to date:
// every row after a resize, otherwise only the rows that changed. Every row
// is positioned explicitly and kept to w-1 visible columns - writing into
// the last column makes some consoles wrap, which would scroll the screen.
func (v *LiveView) frameLocked(w, h int, now time.Time) string {
	rows := v.rowsLocked(w, h, now)

	var b strings.Builder
	full := w != v.lastW || h != v.lastH || len(v.prevRows) != len(rows)
	if full {
		// Resized: wipe the screen and the scrollback, where the console
		// may have reflowed old frames.
		b.WriteString("\x1b[2J\x1b[3J")
		v.lastW, v.lastH = w, h
		v.prevRows = make([]string, len(rows))
	}
	for i, r := range rows {
		if full || r != v.prevRows[i] {
			b.WriteString(r)
			v.prevRows[i] = r
		}
	}
	return b.String()
}

// invalidate forces the next frame to redraw every row.
func (v *LiveView) invalidate() { v.lastW, v.lastH = 0, 0 }

func (v *LiveView) rowsLocked(w, h int, now time.Time) []string {
	cols := w - 1
	logRows := h - 3
	panelW := v.nameW + 7 + detailW
	minLeft := minLogCols
	if v.final != nil {
		// The summary repeats every step's result, so on a window too
		// narrow for both, the summary gets the full width.
		minLeft = max(minLeft, v.finalW)
	}
	showPanel := logRows >= len(v.tasks)+2 && cols-panelW-1 >= minLeft
	logW := cols
	if showPanel {
		logW = cols - panelW - 1
	}
	v.finalRows = logRows

	left := v.paneLinesLocked(logRows)
	rows := make([]string, 0, h)
	for r := 0; r < logRows; r++ {
		row := fmt.Sprintf("\x1b[%d;1H%s\x1b[K", r+1, clipANSI(left[r], logW))
		if showPanel {
			// The divider is placed by jumping to its column, not by
			// padding after the text. If a file name holds a character the
			// terminal draws wider or narrower than counted, the log text
			// may end up off by a column, but the divider and the panel
			// stay in a straight line.
			row += fmt.Sprintf("\x1b[%d;%dH%s│%s%s", r+1, logW+1, DIM, RST,
				clipANSI(v.panelLineLocked(r, now), panelW))
		}
		rows = append(rows, row)
	}

	sep := strings.Repeat("─", logW)
	if showPanel {
		sep += "┴" + strings.Repeat("─", panelW)
	}
	rows = append(rows,
		fmt.Sprintf("\x1b[%d;1H%s%s%s\x1b[K", logRows+1, DIM, sep, RST),
		fmt.Sprintf("\x1b[%d;1H%s\x1b[K", logRows+2, clipANSI(v.statusLocked(cols, now), cols)),
		fmt.Sprintf("\x1b[%d;1H%s\x1b[K", logRows+3, clipANSI(v.footerLocked(cols), cols)),
	)
	return rows
}

// paneLinesLocked returns exactly n colored lines for the left pane: the
// summary once the run is finished, otherwise the tail of the log.
func (v *LiveView) paneLinesLocked(n int) []string {
	out := make([]string, n)
	if v.final != nil {
		v.finalTop = min(v.finalTop, max(0, len(v.final)-n))
		for i := 0; i < n && v.finalTop+i < len(v.final); i++ {
			out[i] = v.final[v.finalTop+i]
		}
		return out
	}
	lines := v.tailLocked(n)
	offset := n - len(lines)
	for i, l := range lines {
		out[offset+i] = renderLogLine(l)
	}
	return out
}

// tailLocked returns the last n log lines, oldest first.
func (v *LiveView) tailLocked(n int) []logLine {
	n = min(n, v.ringLen)
	out := make([]logLine, n)
	start := (v.ringNext - n + logRingSize) % logRingSize
	for i := 0; i < n; i++ {
		out[i] = v.ring[(start+i)%logRingSize]
	}
	return out
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

func (v *LiveView) panelLineLocked(r int, now time.Time) string {
	switch {
	case r == 0:
		return "  " + WHITE + Tf("live.steps", v.startedEnabledLocked(), v.enabled) + RST
	case r == 1 || r-2 >= len(v.tasks):
		return ""
	}

	t := v.tasks[r-2]
	g := v.glyphs
	name := padRight(t.name, v.nameW)
	switch t.state {
	case taskOff:
		return DIM + "  " + g.off + "  " + name + " " + padLeft(T("live.skipped"), detailW) + RST
	case taskNotRun:
		return DIM + "  " + g.off + "  " + name + " " + padLeft(T("common.not_run"), detailW) + RST
	case taskRunning:
		icon := RED + v.spinner(now) + RST
		if v.paused.Load() {
			icon = YELLOW + g.paused + RST
		}
		prog := ""
		if total := v.stepTotal.Load(); total > 0 {
			prog = fmt.Sprintf("%d/%d", v.stepDone.Load(), total)
		}
		return "  " + icon + "  " + WHITE + name + RST + " " + padLeft(prog, detailW)
	case taskDone:
		return "  " + GREEN + g.check + RST + "  " + name + " " + RED + padLeft(t.detail, detailW) + RST
	case taskWarn:
		return "  " + YELLOW + g.warn + RST + "  " + name + " " + YELLOW + padLeft(t.detail, detailW) + RST
	default:
		return DIM + "  " + g.pend + "  " + t.name + RST
	}
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

func (v *LiveView) statusLocked(cols int, now time.Time) string {
	curName, curLabel := "", ""
	if v.cur >= 0 {
		curName = v.tasks[v.cur].name
		curLabel = v.labelFor(v.cur)
	}

	var head string
	aborted := v.aborted.Load()
	switch {
	case v.finished && aborted:
		head = RED + "[" + T("live.aborted") + "] " + curName + RST
	case v.finished:
		head = GREEN + "[" + T("live.done") + "]" + RST
	case aborted:
		head = RED + "[" + T("live.aborting") + "] " + curName + RST
	case v.paused.Load():
		head = YELLOW + "[" + T("live.paused") + "] " + curName + RST
	case v.cur < 0:
		head = RED + v.spinner(now) + RST
	default:
		head = RED + v.spinner(now) + RST + " " + WHITE + curLabel + RST
	}
	// Fixed width, so the bar doesn't jump as task names change length.
	headW := v.nameW + 10
	head += strings.Repeat(" ", max(1, headW-visibleWidth(head)))

	p := v.progressLocked()
	filled := int(p*barW + 0.5)
	bar := RED + strings.Repeat("━", filled) + DIM + strings.Repeat("─", barW-filled) + RST
	pct := fmt.Sprintf(" %3d%%", int(p*100+0.5))

	sepr := " " + DIM + "│" + RST + " "
	stats := sepr + Tf("live.items", v.items.Load()) +
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
	case v.final != nil && len(v.final) > v.finalRows:
		keys = T("live.keys_scroll")
	case v.final != nil:
		keys = T("live.keys_exit")
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
func renderLogLine(l logLine) string {
	switch l.kind {
	case LogHeader:
		return WHITE + l.text + RST
	case LogHeaderAlert:
		return RED + l.text + RST
	case LogOK:
		return RED + "[OK]" + RST + " " + l.text
	case LogWarn:
		return CYAN + "[WARN]" + RST + " " + l.text
	case LogError:
		return RED + "[ERROR]" + RST + " " + l.text
	case LogInfo:
		return CYAN + l.text + RST
	case LogGood:
		return RED + l.text + RST
	case LogBad:
		return YELLOW + l.text + RST
	case LogSkip:
		return l.text + " - " + CYAN + T("common.skipped") + RST
	default:
		return l.text
	}
}

// plainLogLine is the uncolored form used in the log file.
func plainLogLine(l logLine) string {
	return stripANSI(renderLogLine(l))
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

func consoleHWND() uintptr {
	if procGetConsoleWindow.Find() != nil {
		return 0
	}
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

// consoleWindowClass tells the hosts apart: "ConsoleWindowClass" is the
// classic console (conhost); "PseudoConsoleWindow" means a modern terminal
// such as Windows Terminal draws the window.
func consoleWindowClass() string {
	hwnd := consoleHWND()
	if hwnd == 0 || procGetClassNameW.Find() != nil {
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

// Smallest window that shows the live screen comfortably: room for the log
// and the checklist side by side, and every checklist row.
const (
	comfyCols = 120
	comfyRows = 35
)

// EnsureRoomyWindow maximizes a classic console window that is smaller than
// the live screen needs. Windows Terminal ignores this request; there the
// window is opened maximized when Quiesce relaunches as administrator (see
// EnsureAdmin).
func EnsureRoomyWindow() {
	w, h, ok := consoleSize()
	if !ok || (w >= comfyCols && h >= comfyRows) {
		return
	}
	if consoleWindowClass() != "ConsoleWindowClass" || procShowWindow.Find() != nil {
		return
	}
	const swMaximize = 3
	procShowWindow.Call(consoleHWND(), swMaximize)
}

// --- Text width helpers --------------------------------------------------------
//
// Widths are counted in terminal columns: escape sequences take none, and
// CJK/emoji take two. Getting this wrong makes a row wrap, which scrolls the
// whole frame.

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
