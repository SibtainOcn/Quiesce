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

func TestMain(m *testing.M) {
	forcePlainView = true
	os.Exit(m.Run())
}

func stepNames() ([]string, []bool) {
	names := make([]string, 12)
	enabled := make([]bool, 12)
	for i := range names {
		names[i] = T(fmt.Sprintf("step.%d.short", i+1))
		enabled[i] = i != 10 && i != 11
	}
	return names, enabled
}

// newSampleView builds a view part-way through a run, in the given state.
func newSampleView(t *testing.T, mode viewMode, state string) *LiveView {
	t.Helper()
	names, enabled := stepNames()
	var v *LiveView
	captureStdout(t, func() {
		v = NewLiveView(StyleVerbose, T("live.title_clean"), names, enabled, "")
		for i := 0; i < 2; i++ {
			v.BeginTask(i, stepHeaderText(i+1), false)
			v.Deleted("CHROME.EXE-2B1C4F0A.pf", 4096)
			v.EndTask(i, Tf("live.files", 142), false)
		}
		v.BeginTask(2, stepHeaderText(3), false)
		v.SetTotal(180)
		for j := 0; j < 40; j++ {
			v.Deleted(strings.Repeat("very_long_file_name_", 6)+".tmp", 1<<20)
		}
		switch state {
		case "paused":
			v.TogglePause()
		case "aborting":
			v.Abort()
		case "done":
			v.Finish()
		case "aborted":
			v.Abort()
			v.Finish()
		}
	})
	v.mu.Lock()
	v.mode = mode
	v.stats = SystemStats{CpuLoad: 12, UsedGB: 6.21, TotalGB: 15.8, Pct: 39}
	v.mu.Unlock()
	t.Cleanup(func() { captureStdout(t, func() { v.Close() }) })
	return v
}

func drawn(v *LiveView, w, h int) string {
	var b strings.Builder
	v.mu.Lock()
	v.drawLocked(&b, w, h, time.Now())
	v.mu.Unlock()
	return b.String()
}

func sampleLiveFrame(t *testing.T, w, h int) string {
	t.Helper()
	return drawn(newSampleView(t, modeVerbose, "running"), w, h) +
		drawn(newSampleView(t, modeMinimal, "running"), w, h)
}

var cupRe = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// drawnRows maps each row the drawing positions to its visible text.
func drawnRows(frame string) map[int]string {
	rows := map[int]string{}
	locs := cupRe.FindAllStringSubmatchIndex(frame, -1)
	for i, m := range locs {
		row, _ := strconv.Atoi(frame[m[2]:m[3]])
		end := len(frame)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		rows[row] = stripANSI(frame[m[1]:end])
	}
	return rows
}

func TestDrawnRowsFitWidth(t *testing.T) {
	for _, mode := range []viewMode{modeVerbose, modeMinimal} {
		for _, state := range []string{"running", "paused", "aborting", "done", "aborted"} {
			for _, glyphs := range []glyphSet{fancyGlyphs, basicGlyphs} {
				v := newSampleView(t, mode, state)
				v.glyphs = glyphs
				for _, w := range []int{50, 80, 120, 200} {
					for _, h := range []int{20, 40} {
						for row, text := range drawnRows(drawn(v, w, h)) {
							if got := textWidth(text); got > w-1 {
								t.Errorf("mode %d %s %dx%d: row %d is %d columns, max %d: %q",
									mode, state, w, h, row, got, w-1, text)
							}
						}
					}
				}
			}
		}
	}
}

func TestVerbosePinsThreeBottomRows(t *testing.T) {
	v := newSampleView(t, modeVerbose, "running")
	rows := drawnRows(drawn(v, 120, 30))
	for _, r := range []int{28, 29, 30} {
		if _, ok := rows[r]; !ok {
			t.Errorf("row %d not drawn", r)
		}
	}
	if len(rows) != 3 {
		t.Errorf("verbose drew %d rows, want only the 3 pinned ones", len(rows))
	}
	if !strings.Contains(rows[29], "[3/12]") || !strings.Contains(rows[30], T("live.keys_run")) {
		t.Errorf("status/footer rows wrong: %q / %q", rows[29], rows[30])
	}
}

func TestMinimalChecklist(t *testing.T) {
	v := newSampleView(t, modeMinimal, "running")
	v.glyphs = fancyGlyphs
	frame := stripANSI(drawn(v, 120, 30))
	for _, want := range []string{
		T("live.title_clean"), Tf("live.steps", 3, 10),
		"✓", Tf("live.files", 142), "40", T("live.skipped"), T("live.keys_run"),
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("checklist has no %q", want)
		}
	}
	if strings.Contains(frame, "very_long_file_name") {
		t.Error("minimal style shows file names")
	}
}

func TestStatusTags(t *testing.T) {
	cases := map[string]string{
		"paused":  "[" + T("live.paused") + "]",
		"aborted": "[" + T("live.aborted") + "]",
		"done":    "[" + T("live.done") + "]",
	}
	for state, want := range cases {
		for _, mode := range []viewMode{modeVerbose, modeMinimal} {
			if frame := stripANSI(drawn(newSampleView(t, mode, state), 120, 30)); !strings.Contains(frame, want) {
				t.Errorf("mode %d %s: no %q", mode, state, want)
			}
		}
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
		v = NewLiveView(StyleVerbose, T("live.title_clean"), names, enabled, "")
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
