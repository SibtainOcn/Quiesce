package main

// Tests for the localization layer.
//
// These exist to catch the three ways a translation breaks a release, none of
// which the compiler can see:
//
//  1. A key is used in Go code but missing from a locale file, so the raw key
//     ("summary.total") is printed to the user.
//  2. A translator changes the format verbs - drops a %s, or writes %d where
//     the code passes a string - so Printf emits "%!d(string=...)" garbage.
//  3. A translated label is longer than its English source, breaking the
//     ON/OFF column that the menu relies on.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// defaultLang is the source language every other file is compared against.
const defaultLang = "en"

// ansiRe matches the ANSI color escapes used by the UI, so tests can measure
// the visible width of a label rather than its byte length.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// verbRe matches a printf verb. "%%" is excluded by the alternation order:
// it is matched first and then discarded, so it never counts as a verb.
var verbRe = regexp.MustCompile(`%%|%[-+ #0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z]`)

// staticKeyRe finds T("...") and Tf("...") calls in the Go sources.
var staticKeyRe = regexp.MustCompile(`\bTf?\("([^"]+)"`)

// visibleWidth is the rune count of s with color escapes removed.
func visibleWidth(s string) int {
	return len([]rune(ansiRe.ReplaceAllString(s, "")))
}

// verbs returns the printf verbs in s, in order, ignoring "%%".
func verbs(s string) []string {
	out := []string{}
	for _, m := range verbRe.FindAllString(s, -1) {
		if m != "%%" {
			out = append(out, m)
		}
	}
	return out
}

// loadLocale reads one locale file straight from disk (not the embedded copy)
// so a test failure points at the file a translator actually edits.
func loadLocale(t *testing.T, code string) map[string]string {
	t.Helper()
	path := filepath.Join("locales", "active."+code+".toml")
	raw := map[string]interface{}{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	out := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s: key %q is %T, want a string", path, k, v)
		}
		out[k] = s
	}
	return out
}

// localeCodes lists every language file present in locales/.
func localeCodes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("locales")
	if err != nil {
		t.Fatalf("reading locales/: %v", err)
	}
	var codes []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "active.") || !strings.HasSuffix(name, ".toml") {
			continue
		}
		codes = append(codes, strings.TrimSuffix(strings.TrimPrefix(name, "active."), ".toml"))
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		t.Fatal("no locale files found in locales/")
	}
	return codes
}

// usedKeys collects every message key the program can ask for: the literal
// T("...")/Tf("...") calls found in the sources, plus the per-step keys that
// are built at runtime with Sprintf and so cannot be found by scanning.
func usedKeys(t *testing.T) []string {
	t.Helper()

	set := map[string]bool{}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing sources: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		for _, m := range staticKeyRe.FindAllStringSubmatch(string(src), -1) {
			set[m[1]] = true
		}
	}

	// Keys assembled at runtime, e.g. T(fmt.Sprintf("step.%d.name", i)).
	for i := 1; i <= 12; i++ {
		set[fmt.Sprintf("step.%d.name", i)] = true
		set[fmt.Sprintf("step.%d.action", i)] = true
		set[fmt.Sprintf("step.%d.short", i)] = true
	}
	for _, i := range ramSubOptions {
		set[fmt.Sprintf("sub.%d.name", i)] = true
		set[fmt.Sprintf("sub.%d.note", i)] = true
	}

	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestEveryUsedKeyExistsInEnglish is the safety net for a typo'd key: English
// is the final fallback, so a key missing here is printed raw to the user.
func TestEveryUsedKeyExistsInEnglish(t *testing.T) {
	en := loadLocale(t, defaultLang)
	for _, k := range usedKeys(t) {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q is used in code but missing from locales/active.en.toml", k)
		}
	}
}

// TestNoUnusedKeys catches the opposite drift: a key left in the locale files
// after the code that printed it was removed. Translators should not be asked
// to translate strings nothing displays.
func TestNoUnusedKeys(t *testing.T) {
	used := map[string]bool{}
	for _, k := range usedKeys(t) {
		used[k] = true
	}
	for k := range loadLocale(t, defaultLang) {
		if !used[k] {
			t.Errorf("key %q is in locales/active.en.toml but never used in code", k)
		}
	}
}

// TestTranslationsAreComplete requires every non-default language to cover
// every English key. Missing keys still fall back to English at runtime, so
// this is a completeness check, not a crash check - but a half-translated
// screen is a bug worth failing CI over.
func TestTranslationsAreComplete(t *testing.T) {
	en := loadLocale(t, defaultLang)
	for _, code := range localeCodes(t) {
		if code == defaultLang {
			continue
		}
		other := loadLocale(t, code)
		for k := range en {
			if v, ok := other[k]; !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locales/active.%s.toml is missing key %q", code, k)
			}
		}
		for k := range other {
			if _, ok := en[k]; !ok {
				t.Errorf("locales/active.%s.toml has key %q that English does not", code, k)
			}
		}
	}
}

// TestFormatVerbsMatchEnglish is the one that prevents a runtime mess: if a
// translator drops a %s or swaps %d for %s, Printf produces "%!d(string=...)"
// on screen. The verbs must match English exactly, in order.
func TestFormatVerbsMatchEnglish(t *testing.T) {
	en := loadLocale(t, defaultLang)
	for _, code := range localeCodes(t) {
		if code == defaultLang {
			continue
		}
		other := loadLocale(t, code)
		for k, enVal := range en {
			otherVal, ok := other[k]
			if !ok {
				continue // reported by TestTranslationsAreComplete
			}
			want, got := verbs(enVal), verbs(otherVal)
			if strings.Join(want, ",") != strings.Join(got, ",") {
				t.Errorf("locales/active.%s.toml key %q has verbs %v, English has %v",
					code, k, got, want)
			}
		}
	}
}

// TestMenuColumnsAlign is the regression test for the bug this change fixes:
// labels used to carry hand-typed trailing spaces, so any translation longer
// than its English source pushed the ON/OFF column out of line. Every label
// in a group must end up the same visible width, in every language.
func TestMenuColumnsAlign(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	for _, code := range localeCodes(t) {
		SetLanguage(code)

		width := -1
		for i := 1; i <= 12; i++ {
			w := visibleWidth(GetCfgDispName(i))
			if width == -1 {
				width = w
			}
			if w != width {
				t.Errorf("[%s] step %d label is %d wide, expected %d - ON/OFF column would not line up",
					code, i, w, width)
			}
		}

		subWidth := -1
		for _, i := range ramSubOptions {
			w := visibleWidth(GetCfgDispName(i))
			if subWidth == -1 {
				subWidth = w
			}
			if w != subWidth {
				t.Errorf("[%s] sub-option %d label is %d wide, expected %d",
					code, i, w, subWidth)
			}
		}
	}
}

// TestNoTrailingSpacesInLocales enforces the rule the locale files state:
// padding is computed at runtime, so a translator adding trailing spaces
// would silently double the padding.
func TestNoTrailingSpacesInLocales(t *testing.T) {
	for _, code := range localeCodes(t) {
		for k, v := range loadLocale(t, code) {
			if v != strings.TrimRight(v, " \t") {
				t.Errorf("locales/active.%s.toml key %q has trailing whitespace", code, k)
			}
		}
	}
}

// TestUnknownLanguageFallsBackToEnglish covers a bad LANGUAGE= line in the
// config: the app must run in English rather than print raw keys or crash.
func TestUnknownLanguageFallsBackToEnglish(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	SetLanguage(defaultLang)
	want := T("summary.title")

	for _, bad := range []string{"zz", "", "not-a-language", "  "} {
		SetLanguage(bad)
		if got := T("summary.title"); got != want {
			t.Errorf("SetLanguage(%q): got %q, want English %q", bad, got, want)
		}
	}
}

// TestMissingKeyReturnsKey documents the last-resort behaviour: an unknown key
// shows as itself, which is visible in testing rather than silently blank.
func TestMissingKeyReturnsKey(t *testing.T) {
	const key = "this.key.does.not.exist"
	if got := T(key); got != key {
		t.Errorf("T(%q) = %q, want the key itself", key, got)
	}
}

// TestSpanishIsActuallyTranslated guards against a locale file that exists but
// merely copies English - which would pass every other test here.
func TestSpanishIsActuallyTranslated(t *testing.T) {
	original := ActiveLanguage()
	defer SetLanguage(original)

	SetLanguage("es")
	for _, k := range []string{"menu.will_clean", "summary.title", "settings.title", "common.done"} {
		SetLanguage(defaultLang)
		en := T(k)
		SetLanguage("es")
		if es := T(k); es == en {
			t.Errorf("key %q is identical in English and Spanish (%q)", k, es)
		}
	}
}

// TestLanguageOverrideParsing covers the LANGUAGE= line read from the config
// before elevation, including the cases a user is likely to type.
func TestLanguageOverrideParsing(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name, content, want string
	}{
		{"plain", "WIN_TEMP=1\nLANGUAGE=es\n", "es"},
		{"spaced", "LANGUAGE =  es  \n", "es"},
		{"lowercase key", "language=es\n", "es"},
		{"absent", "WIN_TEMP=1\n", ""},
		{"commented out", "# LANGUAGE=es\n", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".dat")
			if err := os.WriteFile(path, []byte(c.content), 0644); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			if got := readLanguageOverride(path); got != c.want {
				t.Errorf("readLanguageOverride = %q, want %q", got, c.want)
			}
		})
	}

	if got := readLanguageOverride(filepath.Join(dir, "does-not-exist.dat")); got != "" {
		t.Errorf("missing file: got %q, want empty", got)
	}
}

// TestPrimaryLangCode checks the LANGID mapping, including sub-languages:
// es-ES (0x0C0A) and es-MX (0x080A) must both resolve to Spanish, or most
// Spanish installs would silently stay on English.
func TestPrimaryLangCode(t *testing.T) {
	cases := map[uint16]string{
		0x0409: "en", // en-US
		0x0809: "en", // en-GB
		0x0C0A: "es", // es-ES
		0x080A: "es", // es-MX
		0x2C0A: "es", // es-AR
		0x040C: "en", // fr-FR - no locale file, falls back
	}
	for langID, want := range cases {
		if got := primaryLangCode(langID); got != want {
			t.Errorf("primaryLangCode(0x%04X) = %q, want %q", langID, got, want)
		}
	}
}

// TestEveryLanguageHasADisplayName keeps the picker honest: a locale file
// added without an entry in languageNames would show up as a bare code like
// "fr" instead of "Français".
func TestEveryLanguageHasADisplayName(t *testing.T) {
	for _, code := range localeCodes(t) {
		name := LanguageDisplayName(code)
		if name == code {
			t.Errorf("language %q has no entry in languageNames in lang.go", code)
		}
		if strings.TrimSpace(name) == "" {
			t.Errorf("language %q has an empty display name", code)
		}
	}
}

// TestAvailableLanguagesMatchesLocaleFiles ensures the picker offers exactly
// the languages that were embedded - no more, no fewer.
func TestAvailableLanguagesMatchesLocaleFiles(t *testing.T) {
	want := strings.Join(localeCodes(t), ",")
	if got := strings.Join(AvailableLanguages(), ","); got != want {
		t.Errorf("AvailableLanguages() = %q, want %q", got, want)
	}
}

// TestKeyLineArgumentCounts pins the number of format verbs on the strings
// the UI formats with a hard-coded argument list. Adding a key to the
// settings line without updating the Tf call would print "%!s(MISSING)" or
// "%!(EXTRA string=...)" across the top of the screen.
func TestKeyLineArgumentCounts(t *testing.T) {
	// key -> number of arguments the call site passes.
	want := map[string]int{
		"settings.keys":      12, // W, S, A, D, L, E - two color args each
		"settings.save_hint": 2,
		"language.keys":      4,
		"language.save_hint": 2,
		"menu.hint":          4,
	}

	for _, code := range localeCodes(t) {
		loc := loadLocale(t, code)
		for key, n := range want {
			v, ok := loc[key]
			if !ok {
				continue // reported elsewhere
			}
			if got := len(verbs(v)); got != n {
				t.Errorf("locales/active.%s.toml key %q has %d verbs, call site passes %d args",
					code, key, got, n)
			}
		}
	}
}

// TestPadRightCountsRunesNotBytes is the accent trap: "Optimización" is 12
// characters but 13 bytes, so byte-based padding would misalign every
// accented label by one column.
func TestPadRightCountsRunesNotBytes(t *testing.T) {
	const s = "Optimización"
	if len(s) == len([]rune(s)) {
		t.Fatal("fixture is not multi-byte; test would prove nothing")
	}
	if got := visibleWidth(padRight(s, 20)); got != 20 {
		t.Errorf("padRight(%q, 20) is %d runes wide, want 20", s, got)
	}
	if got := padRight("already too long for this", 5); got != "already too long for this" {
		t.Errorf("padRight truncated a long label: %q", got)
	}
}
