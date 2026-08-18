package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLanguageSurvivesSave is the regression test for a data-loss bug:
// SaveConfig rewrites the whole file, so before LANGUAGE was round-tripped,
// pressing E on the settings screen silently deleted the user's language
// override and the app reverted to the Windows display language.
func TestLanguageSurvivesSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleaner_config.dat")
	if err := os.WriteFile(path, []byte("WIN_TEMP=1\nLANGUAGE=es\n"), 0644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Language != "es" {
		t.Fatalf("LoadConfig did not read LANGUAGE: got %q, want %q", cfg.Language, "es")
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if got := readLanguageOverride(path); got != "es" {
		t.Errorf("LANGUAGE lost across save: readLanguageOverride = %q, want %q", got, "es")
	}

	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if reloaded.Language != "es" {
		t.Errorf("reloaded language = %q, want %q", reloaded.Language, "es")
	}
}

// TestNoLanguageLineWhenUnset keeps the default config file clean: a user who
// never chose a language should not find a LANGUAGE= line in their config.
func TestNoLanguageLineWhenUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleaner_config.dat")
	if err := SaveConfig(path, DefaultConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if strings.Contains(string(data), "LANGUAGE") {
		t.Errorf("default config should not contain a LANGUAGE line:\n%s", data)
	}
}

// TestLanguageLineDoesNotBreakNumericSettings guards the parser change: the
// non-numeric LANGUAGE value must not make LoadConfig skip or misread the
// numeric settings around it.
func TestLanguageLineDoesNotBreakNumericSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleaner_config.dat")
	content := "WIN_TEMP=0\nLANGUAGE=es\nRECYCLE_BIN=1\nRAM_FILE_CACHE=1\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WinTemp != 0 {
		t.Errorf("WinTemp = %d, want 0", cfg.WinTemp)
	}
	if cfg.RecycleBin != 1 {
		t.Errorf("RecycleBin = %d, want 1 (setting after LANGUAGE was skipped)", cfg.RecycleBin)
	}
	if cfg.RamFileCache != 1 {
		t.Errorf("RamFileCache = %d, want 1", cfg.RamFileCache)
	}
}

// TestDeepCleanupIsNeverPersisted locks in the existing safety rule: the
// destructive step must always start OFF, never resurrected from a config
// file, whether or not that file was written by this version.
func TestDeepCleanupIsNeverPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleaner_config.dat")

	cfg := DefaultConfig()
	cfg.DeepCleanup = 1
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "DEEP") {
		t.Errorf("Deep Cleanup was written to disk:\n%s", data)
	}

	if err := os.WriteFile(path, []byte("DEEP_CLEANUP=1\n"), 0644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if reloaded.DeepCleanup != 0 {
		t.Errorf("DeepCleanup = %d, want 0 - it must never load from disk", reloaded.DeepCleanup)
	}
}

// TestConfigRoundTripsEveryToggle checks that each of the 15 persisted
// settings survives a save/load cycle, so a future edit to the format string
// cannot silently drop one.
func TestConfigRoundTripsEveryToggle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleaner_config.dat")

	indices := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 101, 102, 103, 104}

	cfg := DefaultConfig()
	// Invert every toggle so the test cannot pass on defaults alone.
	for _, i := range indices {
		cfg.SetVal(i, 1-cfg.GetVal(i))
	}
	want := map[int]int{}
	for _, i := range indices {
		want[i] = cfg.GetVal(i)
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	for _, i := range indices {
		if got.GetVal(i) != want[i] {
			t.Errorf("setting %d = %d after round trip, want %d", i, got.GetVal(i), want[i])
		}
	}
}
