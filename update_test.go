package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	rel := releaseInfo{Tag: "v9.0.0", Assets: []releaseAsset{{Name: "other.zip"}, {Name: "qc.exe"}, {Name: "QC-64BIT.EXE"}, {Name: "qc-32bit.exe"}}}
	a, ok := findAsset(rel)
	if !ok || !strings.EqualFold(a.Name, assetNames()[0]) {
		t.Errorf("got %+v %v", a, ok)
	}
	// Older releases only had qc.exe for 64-bit.
	old := releaseInfo{Tag: "v2.5.0", Assets: []releaseAsset{{Name: "qc.exe"}}}
	if a, ok := findAsset(old); runtime.GOARCH != "386" && (!ok || a.Name != "qc.exe") {
		t.Errorf("fallback: got %+v %v", a, ok)
	}
	notes := releaseNotes("- one\r\n\r\n- two\n- three", 2)
	if !strings.Contains(notes, "- one") || !strings.Contains(notes, "...") || strings.Contains(notes, "three") {
		t.Errorf("notes = %q", notes)
	}
}

func TestFriendlyErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "api.github.com"}, "update.err_offline"},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, "update.err_offline"},
		{errStalled, "update.err_timeout"},
		{context.DeadlineExceeded, "update.err_timeout"},
		{errShort, "update.err_dropped"},
		{io.ErrUnexpectedEOF, "update.err_dropped"},
		{&statusError{code: 403, rateLimit: true}, "update.err_rate"},
		{&statusError{code: 404}, "update.err_notfound"},
	}
	for _, c := range cases {
		if got := friendlyError(c.err); got != T(c.want) {
			t.Errorf("%v: got %q, want %q", c.err, got, T(c.want))
		}
	}
	if got := friendlyError(&statusError{code: 503}); !strings.Contains(got, "503") {
		t.Errorf("server error: %q", got)
	}
	if retryable(&net.DNSError{Err: "no such host"}) || !retryable(&statusError{code: 502}) || retryable(&statusError{code: 404}) {
		t.Error("retryable classification is wrong")
	}
}

func TestOfflineCheckFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens here any more

	start := time.Now()
	_, err := downloadFile(releaseAsset{URL: url, Size: 10}, filepath.Join(t.TempDir(), "x"), func(string) {})
	if err == nil || friendlyError(err) != T("update.err_offline") {
		t.Errorf("got %v (%q)", err, friendlyError(err))
	}
	if time.Since(start) > 10*time.Second {
		t.Error("offline failure took too long")
	}
}

func TestStalledDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	r, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newStallReader(r.Body, 200*time.Millisecond, cancel)
	defer s.stop()
	go func() { <-ctx.Done(); r.Body.Close() }()
	_, _ = io.Copy(io.Discard, s)
	if !s.stalled() {
		t.Error("stall not detected")
	}
}

func TestParseChecksums(t *testing.T) {
	h := strings.Repeat("ab", 32)
	sums := parseChecksums(h + "  qc-64bit.exe\r\n" + strings.ToUpper(h) + " *QC-32bit.exe\nbad line\nzz  x.exe\n")
	if sums["qc-64bit.exe"] != h || sums["qc-32bit.exe"] != h || len(sums) != 2 {
		t.Errorf("got %v", sums)
	}
}

func TestDownloadWithoutKnownSize(t *testing.T) {
	body := []byte("MZ unknown size")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	sum := sha256.Sum256(body)
	got, err := downloadFile(releaseAsset{URL: srv.URL}, filepath.Join(t.TempDir(), "a"), func(string) {})
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Errorf("hash %q, err %v", got, err)
	}
}

func TestUpdateHintOffWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoUpdate = 0
	if UpdateHint(cfg) != "" {
		t.Error("hint shown with checks off")
	}
}
