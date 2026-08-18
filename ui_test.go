package main

// Screen-rendering tests.
//
// These capture what the program actually prints and assert on the layout,
// because the alignment bugs this package is prone to are invisible to the
// compiler and only show up as a crooked column on someone else's machine.

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// rowRe matches a numbered screen row, e.g. "  [10] RAM Optimization   : ...".
var rowRe = regexp.MustCompile(`^\s*\[\d+\]`)

// captureStdout runs fn with os.Stdout redirected, and returns what was
// printed with ANSI color escapes stripped.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	func() {
		defer func() {
			os.Stdout = orig
			w.Close()
		}()
		fn()
	}()

	return ansiRe.ReplaceAllString(<-done, "")
}

// colonColumns returns the rune offset of the " : " separator on every
// numbered row in the captured output.
func colonColumns(out string) map[string]int {
	cols := map[string]int{}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if !rowRe.MatchString(ln) {
			continue
		}
		if i := strings.Index(ln, " : "); i >= 0 {
			cols[ln] = len([]rune(ln[:i]))
		}
	}
	return cols
}

// sampleResults builds a StepResults with every branch of the summary
// populated, so the rendering tests exercise real output rather than zeros.
func sampleResults() StepResults {
	return StepResults{
		TotalDeleted: 1234,
		TotalFailed:  7,
		StepCounts:   [12]int64{0, 400, 300, 120, 30, 90, 200, 80, 14, 0, 0, 0},
		Ram: RamResult{
			FreedMB:   2048,
			BeforePct: 78.4,
			AfterPct:  61.2,
			DropPct:   17.2,
			OpsRun:    []string{"flush", "standby"},
		},
		RecycleBinBytes:   1048576,
		DeepCleanupRan:    true,
		DeepCleanupFailed: false,
	}
}

// TestMainMenuColumnsAlignOnScreen renders the real main menu and checks that
// every numbered row puts its " : " in the same column. The sub-options form
// their own indented group, so they are allowed their own column.
func TestMainMenuColumnsAlignOnScreen(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	cfg := DefaultConfig()
	cfg.DeepCleanup = 1

	for _, code := range localeCodes(t) {
		SetLanguage(code)
		out := captureStdout(t, func() {
			DrawMainMenu(cfg, "Microsoft Windows ", "Version 10.0.0", "PC", "user")
		})

		want := -1
		for line, col := range colonColumns(out) {
			if want == -1 {
				want = col
			}
			if col != want {
				t.Errorf("[%s] main menu column mismatch (%d vs %d): %q", code, col, want, line)
			}
		}
		if want == -1 {
			t.Errorf("[%s] main menu produced no numbered rows", code)
		}
	}
}

// TestSummaryColumnsAlignOnScreen is the regression test for the Deep Cleanup
// row, which carried hand-typed spacing and sat one column off from every
// other summary row even in English.
func TestSummaryColumnsAlignOnScreen(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	cfg := DefaultConfig()
	cfg.DeepCleanup = 1
	results := sampleResults()

	for _, code := range localeCodes(t) {
		SetLanguage(code)
		out := captureStdout(t, func() { PrintSummary(cfg, results) })

		want := -1
		for line, col := range colonColumns(out) {
			if want == -1 {
				want = col
			}
			if col != want {
				t.Errorf("[%s] summary column mismatch (%d vs %d): %q", code, col, want, line)
			}
		}
		if want == -1 {
			t.Errorf("[%s] summary produced no numbered rows", code)
		}
	}
}

// TestNoRawKeysOnScreen is the end-to-end guard against a mistyped key: if a
// lookup fails, T returns the key itself, which would appear on screen as
// literal text like "summary.total". Nothing the user sees should look like
// a dotted identifier.
func TestNoRawKeysOnScreen(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	// Matches bare dotted identifiers such as "cleaner.bin_freed". URLs and
	// filenames on screen (github.com/..., cleanmgr.exe) are excluded by
	// requiring the whole token to be a key-shaped word.
	keyish := regexp.MustCompile(`(?m)(^|\s)[a-z]+(\.[a-z0-9_]+){1,3}(\s|$)`)

	allowed := []string{"cleanmgr.exe", "qc.exe", "github.com", "cleaner_config.dat"}

	cfg := DefaultConfig()
	cfg.DeepCleanup = 1
	results := sampleResults()

	for _, code := range localeCodes(t) {
		SetLanguage(code)

		out := captureStdout(t, func() {
			DrawMainMenu(cfg, "Microsoft Windows ", "Version 10.0.0", "PC", "user")
			PrintSummary(cfg, results)
			for i := 1; i <= 12; i++ {
				StepSkipped(i)
			}
			PrintHelp()
		})

		for _, m := range keyish.FindAllString(out, -1) {
			token := strings.TrimSpace(m)
			skip := false
			for _, a := range allowed {
				if strings.Contains(token, a) {
					skip = true
					break
				}
			}
			if !skip {
				t.Errorf("[%s] untranslated key leaked to screen: %q", code, token)
			}
		}
	}
}

// TestQuickEditDisabledMode covers the console flag arithmetic behind the
// click-to-freeze fix. Clicking the window used to put the console into
// selection mode, which blocks every write - so the app appeared to hang,
// and could not even print a warning, because printing was what was blocked.
//
// The syscall itself cannot be tested here: `go test` redirects the standard
// handles, so there is no console to query. The bit maths is the part that
// can silently go wrong, and it is what this checks.
func TestQuickEditDisabledMode(t *testing.T) {
	const (
		processedInput = 0x0001 // ENABLE_PROCESSED_INPUT
		mouseInput     = 0x0010 // ENABLE_MOUSE_INPUT
	)

	cases := []struct {
		name string
		in   uint32
	}{
		{"typical default console", processedInput | mouseInput | ENABLE_QUICK_EDIT_MODE},
		{"quick edit already off", processedInput | mouseInput},
		{"extended flags already set", ENABLE_EXTENDED_FLAGS | ENABLE_QUICK_EDIT_MODE},
		{"no flags at all", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := quickEditDisabledMode(c.in)

			if got&ENABLE_QUICK_EDIT_MODE != 0 {
				t.Error("QuickEdit is still enabled - clicking the window would still freeze the app")
			}
			// Without this flag Windows ignores the QuickEdit change entirely.
			if got&ENABLE_EXTENDED_FLAGS == 0 {
				t.Error("ENABLE_EXTENDED_FLAGS not set - the QuickEdit change would be ignored")
			}
			// Unrelated settings belong to the user, not to us.
			preserved := c.in &^ (ENABLE_QUICK_EDIT_MODE | ENABLE_EXTENDED_FLAGS)
			if got&preserved != preserved {
				t.Errorf("other console flags were lost: input 0x%X, output 0x%X", c.in, got)
			}
		})
	}
}

// TestRestoreConsoleModeIsSafeWithoutAConsole documents that exiting must not
// panic when the mode was never captured - which is the case for every test
// run, and for any environment where the handle is redirected.
func TestRestoreConsoleModeIsSafeWithoutAConsole(t *testing.T) {
	saved, savedFlag := origInputMode, origInputModeSaved
	defer func() { origInputMode, origInputModeSaved = saved, savedFlag }()

	origInputMode, origInputModeSaved = 0, false
	RestoreConsoleMode() // must be a no-op, not a crash
	RestoreConsoleMode()
}

// TestCenterInBox keeps headings inside the 41-column rule and centered.
func TestCenterInBox(t *testing.T) {
	for _, s := range []string{"CLEANING SUMMARY", "CONFIGURAR OPCIONES DE LIMPIEZA", "X"} {
		got := centerInBox(s)
		if !strings.HasSuffix(got, s) {
			t.Errorf("centerInBox(%q) = %q, want it to end with the input", s, got)
		}
		if len([]rune(got)) > boxWidth {
			t.Errorf("centerInBox(%q) is %d wide, exceeds boxWidth %d", s, len([]rune(got)), boxWidth)
		}
		lead := len([]rune(got)) - len([]rune(s))
		if want := (boxWidth - len([]rune(s))) / 2; lead != want {
			t.Errorf("centerInBox(%q) indented %d, want %d", s, lead, want)
		}
	}
}
