package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain keeps every test off the real console: with the live screen on, a
// test run from a terminal would clear it and read the keyboard.
func TestMain(m *testing.M) {
	forcePlainView = true
	os.Exit(m.Run())
}

func stepNames() ([]string, []bool) {
	names := make([]string, 12)
	enabled := make([]bool, 12)
	for i := range names {
		names[i] = T(fmt.Sprintf("step.%d.short", i+1))
		enabled[i] = i != 10 && i != 11 // Recycle Bin and Deep Cleanup off
	}
	return names, enabled
}

// newSampleView builds a view part-way through a run, in the given state.
func newSampleView(t *testing.T, state string) *LiveView {
	t.Helper()
	names, enabled := stepNames()
	var v *LiveView
	captureStdout(t, func() {
		v = NewLiveView(names, enabled, "")
		for i := 0; i < 2; i++ {
			v.BeginTask(i, stepHeaderText(i+1), false)
			v.Deleted("CHROME.EXE-2B1C4F0A.pf", 4096)
			v.EndTask(i, "142", false)
		}
		v.BeginTask(2, stepHeaderText(3), false)
		v.SetTotal(180)
		for j := 0; j < 40; j++ {
			v.Deleted(strings.Repeat("very_long_file_name_", 6)+".tmp", 1<<20)
		}
		v.Log(LogWarn, "a warning line that is much longer than any pane could ever be, to force clipping")
		v.Log(LogPlain, "日本語のファイル名がとても長い場合でも折り返さないこと.tmp")
		switch state {
		case "paused":
			v.TogglePause()
		case "aborting":
			v.Abort()
		case "done":
			v.Close()
		case "aborted":
			v.Abort()
			v.Close()
		}
	})
	v.mu.Lock()
	v.stats = SystemStats{CpuLoad: 12, UsedGB: 6.21, TotalGB: 15.8, Pct: 39}
	v.mu.Unlock()
	return v
}

func sampleLiveFrame(t *testing.T, w, h int) string {
	t.Helper()
	v := newSampleView(t, "running")
	defer v.Close()
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.frameLocked(w, h, time.Now())
}

var cupRe = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// frameRows splits a frame into rows and returns, for each row, the column
// just past its last visible character. Text after a cursor jump starts at
// that jump's column.
func frameRows(frame string) map[int]int {
	ends := map[int]int{}
	locs := cupRe.FindAllStringSubmatchIndex(frame, -1)
	for i, m := range locs {
		row, _ := strconv.Atoi(frame[m[2]:m[3]])
		col, _ := strconv.Atoi(frame[m[4]:m[5]])
		end := len(frame)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		seg := strings.ReplaceAll(frame[m[1]:end], "\x1b[K", "")
		ends[row] = max(ends[row], col-1+visibleWidth(seg))
	}
	return ends
}

// dividerCols returns the column of the panel divider on each row.
func dividerCols(frame string) map[int]int {
	cols := map[int]int{}
	for _, m := range cupRe.FindAllStringSubmatchIndex(frame, -1) {
		if strings.HasPrefix(stripANSI(frame[m[1]:]), "│") {
			row, _ := strconv.Atoi(frame[m[2]:m[3]])
			col, _ := strconv.Atoi(frame[m[4]:m[5]])
			cols[row] = col
		}
	}
	return cols
}

// TestDividerIsStraight: the divider must sit in the same column on every
// row whatever the log lines contain, including wide characters.
func TestDividerIsStraight(t *testing.T) {
	v := newSampleView(t, "running")
	defer v.Close()
	v.mu.Lock()
	frame := v.frameLocked(140, 30, time.Now())
	v.mu.Unlock()

	cols := dividerCols(frame)
	if len(cols) != 27 {
		t.Fatalf("divider drawn on %d rows, want 27", len(cols))
	}
	want := -1
	for row, c := range cols {
		if want == -1 {
			want = c
		}
		if c != want {
			t.Errorf("row %d: divider at column %d, want %d", row, c, want)
		}
	}
}

// TestLiveFrameRowsFitWidth is the guard against the frame scrolling: a row
// that reaches the last column wraps on some consoles and pushes the whole
// screen up one line every redraw.
func TestLiveFrameRowsFitWidth(t *testing.T) {
	for _, state := range []string{"running", "paused", "aborting", "done", "aborted"} {
		for _, glyphs := range []glyphSet{fancyGlyphs, basicGlyphs} {
			v := newSampleView(t, state)
			v.glyphs = glyphs
			for _, w := range []int{60, 80, 100, 140, 200} {
				for _, h := range []int{12, 20, 40} {
					v.invalidate()
					v.mu.Lock()
					frame := v.frameLocked(w, h, time.Now())
					v.mu.Unlock()

					rows := frameRows(frame)
					if len(rows) != h {
						t.Errorf("%s %dx%d: drew %d rows, want %d", state, w, h, len(rows), h)
					}
					for row, end := range rows {
						if end > w-1 {
							t.Errorf("%s %dx%d: row %d reaches column %d, max %d",
								state, w, h, row, end, w-1)
						}
					}
				}
			}
			v.Close()
		}
	}
}

func TestLiveFrameShowsTags(t *testing.T) {
	cases := map[string]string{
		"paused":  "[" + T("live.paused") + "]",
		"aborted": "[" + T("live.aborted") + "]",
		"done":    "[" + T("live.done") + "]",
	}
	for state, want := range cases {
		v := newSampleView(t, state)
		v.mu.Lock()
		frame := stripANSI(v.frameLocked(120, 30, time.Now()))
		v.mu.Unlock()
		v.Close()
		if !strings.Contains(frame, want) {
			t.Errorf("%s: frame has no %q", state, want)
		}
	}
}

// TestSummaryStaysInsideLayout: the end-of-run summary is drawn in the log
// pane, so the checklist, status line and footer stay pinned and nothing is
// printed below the screen.
func TestSummaryStaysInsideLayout(t *testing.T) {
	cfg := DefaultConfig()
	lines := SummaryLines(cfg, sampleResults())
	v := newSampleView(t, "done")
	defer v.Close()

	const w, h = 140, 30
	v.mu.Lock()
	v.final = lines
	frame := v.frameLocked(w, h, time.Now())
	v.mu.Unlock()

	rows := frameRows(frame)
	if len(rows) != h {
		t.Fatalf("summary frame drew %d rows, want %d", len(rows), h)
	}
	if got := len(dividerCols(frame)); got != h-3 {
		t.Errorf("checklist divider on %d rows during summary, want %d", got, h-3)
	}
	plain := stripANSI(frame)
	for _, want := range []string{T("summary.title"), T("live.keys_scroll"), "[" + T("live.done") + "]"} {
		if !strings.Contains(plain, want) {
			t.Errorf("summary frame has no %q", want)
		}
	}

	// Scrolling to the end shows the last summary line, and stops there.
	v.scrollSummary(1 << 20)
	v.mu.Lock()
	v.invalidate()
	frame = stripANSI(v.frameLocked(w, h, time.Now()))
	top := v.finalTop
	v.mu.Unlock()
	if last := strings.TrimSpace(stripANSI(lines[len(lines)-1])); !strings.Contains(frame, last) {
		t.Errorf("after scrolling to the end, %q is not on screen", last)
	}
	if want := len(lines) - (h - 3); top != want {
		t.Errorf("scrolled to line %d, want %d", top, want)
	}
	v.scrollSummary(-1 << 20)
	if v.finalTop != 0 {
		t.Errorf("scrolling up past the start left finalTop at %d", v.finalTop)
	}
}

// TestUnchangedRowsAreNotRewritten: once drawn, a frame with nothing new
// writes only the rows that change (the spinner), not the whole screen.
func TestUnchangedRowsAreNotRewritten(t *testing.T) {
	v := newSampleView(t, "done")
	defer v.Close()
	now := time.Now()
	v.mu.Lock()
	first := v.frameLocked(120, 30, now)
	second := v.frameLocked(120, 30, now)
	v.mu.Unlock()
	if first == "" || second != "" {
		t.Errorf("identical frame redrew %d bytes, want 0", len(second))
	}
}

func TestStoppedServicesAreTracked(t *testing.T) {
	markServiceStopped("TestSvcA")
	markServiceStopped("TestSvcB")
	markServiceRestarted("TestSvcA")
	svcMu.Lock()
	_, a := stoppedServices["TestSvcA"]
	_, b := stoppedServices["TestSvcB"]
	svcMu.Unlock()
	markServiceRestarted("TestSvcB")
	if a || !b {
		t.Errorf("tracking wrong: A stopped=%v (want false), B stopped=%v (want true)", a, b)
	}
}

func TestClipANSI(t *testing.T) {
	s := RED + "[OK]" + RST + " abcdef"
	if got := stripANSI(clipANSI(s, 6)); got != "[OK] a" {
		t.Errorf("clip to 6 = %q", got)
	}
	if got := clipANSI(s, 50); got != s {
		t.Errorf("unclipped string changed: %q", got)
	}
	if got := visibleWidth(clipANSI("日本語", 5)); got != 4 {
		t.Errorf("wide runes clipped to width %d, want 4", got)
	}
}

func TestRunTasksIsolatesPanics(t *testing.T) {
	ranAfter := false
	var rep RunReport
	captureStdout(t, func() {
		rep = RunTasks([]Task{
			{Name: "boom", Title: "boom", Enabled: true, Run: func(*TaskCtx) TaskOutcome { panic("test panic") }},
			{Name: "next", Title: "next", Enabled: true, Run: func(*TaskCtx) TaskOutcome { ranAfter = true; return TaskOutcome{} }},
		}, RunOptions{})
	})
	if !ranAfter || !rep.Ran[0] || !rep.Ran[1] {
		t.Errorf("a panicking task stopped the run: ran=%v after=%v", rep.Ran, ranAfter)
	}
}

func TestRunTasksAbortSkipsTheRest(t *testing.T) {
	ranSecond := false
	var rep RunReport
	captureStdout(t, func() {
		rep = RunTasks([]Task{
			{Name: "a", Title: "a", Enabled: true, Run: func(tc *TaskCtx) TaskOutcome { tc.v.Abort(); return TaskOutcome{} }},
			{Name: "b", Title: "b", Enabled: true, Run: func(*TaskCtx) TaskOutcome { ranSecond = true; return TaskOutcome{} }},
		}, RunOptions{})
	})
	if !rep.Aborted || ranSecond || rep.Ran[1] {
		t.Errorf("abort did not stop the run: aborted=%v ran=%v", rep.Aborted, rep.Ran)
	}
}

func TestPauseBlocksUntilResumed(t *testing.T) {
	names, enabled := stepNames()
	var v *LiveView
	captureStdout(t, func() {
		v = NewLiveView(names, enabled, "")
		v.TogglePause()
	})
	defer v.Close()

	done := make(chan bool)
	go func() { done <- v.Checkpoint() }()
	select {
	case <-done:
		t.Fatal("Checkpoint returned while paused")
	case <-time.After(100 * time.Millisecond):
	}
	captureStdout(t, v.TogglePause)
	select {
	case ok := <-done:
		if !ok {
			t.Error("Checkpoint reported abort after a plain resume")
		}
	case <-time.After(time.Second):
		t.Fatal("Checkpoint still blocked after resume")
	}
}

func makeFiles(t *testing.T, root string, files map[string]int) {
	t.Helper()
	for p, n := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanDirRemovesContentsAndCountsBytes(t *testing.T) {
	root := t.TempDir()
	makeFiles(t, root, map[string]int{"a.tmp": 100, "b.tmp": 200, `sub\c.tmp`: 300, `sub\deep\d.tmp`: 400})

	var rep RunReport
	var d, f int64
	captureStdout(t, func() {
		rep = RunTasks([]Task{{Name: "x", Title: "x", Enabled: true, Run: func(tc *TaskCtx) TaskOutcome {
			d, f = tc.CleanDir(root)
			return TaskOutcome{}
		}}}, RunOptions{})
	})
	if d != 3 || f != 0 {
		t.Errorf("deleted=%d failed=%d, want 3 and 0 (two files and one folder)", d, f)
	}
	if rep.Bytes != 1000 {
		t.Errorf("bytes freed = %d, want 1000", rep.Bytes)
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Errorf("%d entries left in the folder", len(left))
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the folder itself was removed: %v", err)
	}
}

func TestCleanTreeKeepsFolders(t *testing.T) {
	root := t.TempDir()
	makeFiles(t, root, map[string]int{"a.log": 10, `x\b.log`: 20, `x\y\c.log`: 30})

	var d int64
	captureStdout(t, func() {
		RunTasks([]Task{{Name: "x", Title: "x", Enabled: true, Run: func(tc *TaskCtx) TaskOutcome {
			d, _ = tc.CleanTree(root)
			return TaskOutcome{}
		}}}, RunOptions{})
	})
	if d != 3 {
		t.Errorf("deleted %d files, want 3", d)
	}
	if _, err := os.Stat(filepath.Join(root, "x", "y")); err != nil {
		t.Errorf("folder structure was not kept: %v", err)
	}
}

func TestMissingFolderIsQuiet(t *testing.T) {
	var d, f int64
	out := captureStdout(t, func() {
		RunTasks([]Task{{Name: "x", Title: "x", Enabled: true, Run: func(tc *TaskCtx) TaskOutcome {
			d, f = tc.CleanDir(filepath.Join(t.TempDir(), "does-not-exist"))
			return TaskOutcome{}
		}}}, RunOptions{})
	})
	if d != 0 || f != 0 || strings.Contains(out, "[WARN]") {
		t.Errorf("missing folder: deleted=%d failed=%d output=%q", d, f, out)
	}
}

func TestRunLogIsWritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	var rep RunReport
	captureStdout(t, func() {
		rep = RunTasks([]Task{{Name: "x", Title: "Cleaning x...", Enabled: true, Run: func(tc *TaskCtx) TaskOutcome {
			tc.v.Deleted("file.tmp", 10)
			return TaskOutcome{}
		}}}, RunOptions{LogDir: dir})
	})
	if rep.LogErr != nil || rep.LogPath == "" {
		t.Fatalf("log not written: path=%q err=%v", rep.LogPath, rep.LogErr)
	}
	b, err := os.ReadFile(rep.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{AppName, "Cleaning x...", "[OK] file.tmp"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log file has no %q", want)
		}
	}
	if strings.Contains(string(b), "\x1b") {
		t.Error("log file contains color escape codes")
	}
}

func TestRunLogFailureIsReported(t *testing.T) {
	// A file where the logs folder should be makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "logs")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var rep RunReport
	captureStdout(t, func() {
		rep = RunTasks([]Task{{Name: "x", Title: "x", Enabled: true, Run: func(*TaskCtx) TaskOutcome { return TaskOutcome{} }}}, RunOptions{LogDir: blocker})
	})
	if rep.LogErr == nil {
		t.Error("expected a log error, got none")
	}
}
