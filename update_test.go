package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2.6.0", "2.5.0", true},
		{"2.5.1", "2.5.0", true},
		{"v3", "2.9.9", true},
		{"v2.5.0", "2.5.0", false},
		{"v2.4.9", "2.5.0", false},
		{"v2.10.0", "2.9.0", true},
		{"nightly", "2.5.0", false},
		{"v2.6.0-rc1", "2.5.0", false},
		{"", "2.5.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestDownloadFileChecksSizeAndHash(t *testing.T) {
	body := []byte("MZ fake binary contents")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	sum := sha256.Sum256(body)

	got, err := downloadFile(releaseAsset{URL: srv.URL, Size: int64(len(body))}, filepath.Join(dir, "a"), func(string) {})
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %q, err %v", got, err)
	}

	if _, err := downloadFile(releaseAsset{URL: srv.URL, Size: int64(len(body)) - 1}, filepath.Join(dir, "b"), func(string) {}); err == nil {
		t.Error("oversized download accepted")
	}
	if _, err := downloadFile(releaseAsset{URL: srv.URL, Size: int64(len(body)) + 5}, filepath.Join(dir, "c"), func(string) {}); err == nil {
		t.Error("truncated download accepted")
	}
}

func TestSwapBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "qc.exe")
	newer := exe + ".new"
	_ = os.WriteFile(exe, []byte("old"), 0644)
	_ = os.WriteFile(newer, []byte("new"), 0644)

	if err := swapBinary(exe, newer); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("exe = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Errorf("old = %q", b)
	}

	// A missing new file must leave the current exe in place.
	if err := swapBinary(exe, filepath.Join(dir, "missing")); err == nil {
		t.Error("expected an error")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("exe after failed swap = %q", b)
	}
}

func TestFindAssetAndNotes(t *testing.T) {
	rel := releaseInfo{Tag: "v9.0.0", Assets: []releaseAsset{{Name: "other.zip"}, {Name: "QC.EXE"}, {Name: "qc-386.exe"}}}
	a, ok := findAsset(rel)
	if !ok || !strings.EqualFold(a.Name, assetName()) {
		t.Errorf("got %+v %v", a, ok)
	}
	notes := releaseNotes("- one\r\n\r\n- two\n- three", 2)
	if !strings.Contains(notes, "- one") || !strings.Contains(notes, "...") || strings.Contains(notes, "three") {
		t.Errorf("notes = %q", notes)
	}
}

func TestUpdateHintOffWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoUpdate = 0
	if UpdateHint(cfg) != "" {
		t.Error("hint shown with checks off")
	}
}
