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

// Self-update. A background check (at most once a day, cached on disk) feeds
// the "update available" hint; `qc -u` downloads the latest release, checks
// its SHA-256 against the digest GitHub publishes, test-runs it, and swaps
// it in with a rollback if any step fails.

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	latestReleaseAPIDefault = "https://api.github.com/repos/SibtainOcn/Quiesce/releases/latest"
	updateCheckEvery = 24 * time.Hour
	maxDownloadSize  = 64 << 20
	createNoWindow   = 0x08000000
)

// latestReleaseAPI is a var so tests can point it elsewhere.
var latestReleaseAPI = latestReleaseAPIDefault

var (
	errStalled = errors.New("download stalled")
	errShort   = errors.New("download incomplete")
)

const stallTimeout = 30 * time.Second

type releaseAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

type releaseInfo struct {
	Tag    string         `json:"tag_name"`
	Body   string         `json:"body"`
	Assets []releaseAsset `json:"assets"`
}

func (r releaseInfo) version() string { return strings.TrimPrefix(r.Tag, "v") }

// parseVersion reads "v2.5.0" or "2.5" into numbers; ok is false for
// anything else, so odd tags never trigger an update.
func parseVersion(s string) (v [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// newerVersion reports whether latest is a higher version than current.
func newerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// assetNames lists the release files for this build, preferred first.
// Releases up to v2.5.0 shipped the 64-bit build as plain qc.exe.
func assetNames() []string {
	if runtime.GOARCH == "386" {
		return []string{"qc-32bit.exe"}
	}
	return []string{"qc-64bit.exe", "qc.exe"}
}

// httpClient has no overall timeout: downloads on slow links can take a
// while, so stalls are caught per read instead (see stallReader).
var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	},
}

// statusError is a non-200 reply from GitHub.
type statusError struct {
	code      int
	rateLimit bool
}

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func checkStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	limited := (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")
	return &statusError{code: resp.StatusCode, rateLimit: limited}
}

// retryable is true for errors worth another try: timeouts, dropped
// connections and GitHub server errors. Being offline is not.
func retryable(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		return se.code >= 500
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && !dnsErr.IsTimeout {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

// friendlyError turns network and HTTP failures into a short, plain reason.
func friendlyError(err error) string {
	var se *statusError
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var certErr *tls.CertificateVerificationError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &se) && se.rateLimit:
		return T("update.err_rate")
	case errors.As(err, &se) && se.code == http.StatusNotFound:
		return T("update.err_notfound")
	case errors.As(err, &se) && se.code >= 500:
		return Tf("update.err_server", se.code)
	case errors.As(err, &se):
		return Tf("update.err_http", se.code)
	case errors.Is(err, errStalled), errors.Is(err, context.DeadlineExceeded):
		return T("update.err_timeout")
	case errors.As(err, &certErr):
		return T("update.err_tls")
	case errors.As(err, &dnsErr), errors.As(err, &opErr) && opErr.Op == "dial":
		return T("update.err_offline")
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET), errors.Is(err, errShort):
		return T("update.err_dropped")
	case errors.Is(err, windows.ERROR_DISK_FULL), errors.Is(err, windows.ERROR_HANDLE_DISK_FULL):
		return T("update.err_disk")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return T("update.err_timeout")
	}
	return err.Error()
}

func fetchLatestRelease(ctx context.Context) (rel releaseInfo, err error) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return rel, err
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		rel, err = fetchLatestOnce(ctx)
		if err == nil || !retryable(err) || ctx.Err() != nil {
			return rel, err
		}
	}
	return rel, err
}

// isOffline is true when the network itself is unreachable, so trying
// another GitHub address would not help.
func isOffline(err error) bool {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	return errors.As(err, &dnsErr) || errors.As(err, &opErr) && opErr.Op == "dial"
}

// noRedirectClient reports redirects instead of following them.
var noRedirectClient = &http.Client{
	Transport:     httpClient.Transport,
	Timeout:       30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// latestTagViaRedirect reads the newest release tag from where
// github.com/<repo>/releases/latest redirects to. Web pages are not
// subject to the API's 60-requests-an-hour limit.
func latestTagViaRedirect(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", &statusError{code: resp.StatusCode}
	}
	_, tag, ok := strings.Cut(resp.Header.Get("Location"), "/releases/tag/")
	if !ok {
		return "", &statusError{code: http.StatusNotFound}
	}
	if _, ok := parseVersion(tag); !ok {
		return "", fmt.Errorf("unexpected release tag %q", tag)
	}
	return tag, nil
}

// releaseFromChecksums builds release info for tag from its SHA256SUMS.txt.
// Without that file the assets carry no digest, and qc -u refuses them.
func releaseFromChecksums(ctx context.Context, tag string) (releaseInfo, error) {
	rel := releaseInfo{Tag: tag}
	base := Repo + "/releases/download/" + tag + "/"
	for _, n := range assetNames() {
		rel.Assets = append(rel.Assets, releaseAsset{Name: n, URL: base + n})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"SHA256SUMS.txt", nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return rel, nil
	}
	if err := checkStatus(resp); err != nil {
		return rel, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return rel, err
	}
	sums := parseChecksums(string(data))
	for i, a := range rel.Assets {
		if h, ok := sums[strings.ToLower(a.Name)]; ok {
			rel.Assets[i].Digest = "sha256:" + h
		}
	}
	return rel, nil
}

// parseChecksums reads sha256sum output ("<hash>  name" or "<hash> *name").
func parseChecksums(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		h := strings.ToLower(fields[0])
		if _, err := hex.DecodeString(h); err != nil || len(h) != 64 {
			continue
		}
		out[strings.ToLower(strings.TrimPrefix(fields[1], "*"))] = h
	}
	return out
}

// latestForUpdate gets the release to install: the API first, then the
// redirect and checksum file when the API refuses (rate limit, outage).
func latestForUpdate(ctx context.Context) (releaseInfo, error) {
	rel, err := fetchLatestRelease(ctx)
	if err == nil || isOffline(err) || ctx.Err() != nil {
		return rel, err
	}
	tag, rerr := latestTagViaRedirect(ctx)
	if rerr != nil {
		return rel, err
	}
	return releaseFromChecksums(ctx, tag)
}

func fetchLatestOnce(ctx context.Context) (releaseInfo, error) {
	var rel releaseInfo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return rel, err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return rel, fmt.Errorf("unreadable reply from GitHub: %w", err)
	}
	if _, ok := parseVersion(rel.Tag); !ok {
		return rel, fmt.Errorf("unexpected release tag %q", rel.Tag)
	}
	return rel, nil
}

// --- Cached background check ------------------------------------------------

type updateCache struct {
	LastCheck time.Time
	Latest    string
}

func updateCachePath() string {
	return filepath.Join(filepath.Dir(ConfigFilePath()), "qc_update.dat")
}

func readUpdateCache() updateCache {
	var c updateCache
	data, err := os.ReadFile(updateCachePath())
	if err != nil {
		return c
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "LAST_CHECK":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				c.LastCheck = time.Unix(n, 0)
			}
		case "LATEST":
			if _, ok := parseVersion(v); ok {
				c.Latest = v
			}
		}
	}
	return c
}

// writeUpdateCache writes through a temp file so a killed process never
// leaves a half-written cache.
func writeUpdateCache(c updateCache) {
	path := updateCachePath()
	tmp := path + ".tmp"
	content := fmt.Sprintf("LAST_CHECK=%d\nLATEST=%s\n", c.LastCheck.Unix(), c.Latest)
	if os.WriteFile(tmp, []byte(content), 0644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// StartUpdateCheck refreshes the cache in the background when it is older
// than a day. It never blocks or prints; the hint reads the cache.
func StartUpdateCheck(cfg *Config) {
	if cfg.AutoUpdate != 1 {
		return
	}
	c := readUpdateCache()
	if since := time.Since(c.LastCheck); since >= 0 && since < updateCheckEvery {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		c.LastCheck = time.Now()
		if tag, err := latestTagViaRedirect(ctx); err == nil {
			c.Latest = strings.TrimPrefix(tag, "v")
		} else {
			// Offline or failed: try again in about an hour, not a day.
			c.LastCheck = c.LastCheck.Add(-updateCheckEvery + time.Hour)
		}
		writeUpdateCache(c)
	}()
}

// UpdateHint is the one-line notice, or "" when there is nothing newer.
func UpdateHint(cfg *Config) string {
	if cfg.AutoUpdate != 1 {
		return ""
	}
	latest := readUpdateCache().Latest
	if !newerVersion(latest, Version) {
		return ""
	}
	return YELLOW + Tf("update.hint", Version, latest, AppShort) + RST
}

// AskAutoUpdate is the first-run question. ENTER or Y means yes.
func AskAutoUpdate(cfg *Config) {
	fmt.Printf("\n  %s ", T("update.first_run"))
	FlushKeyBuffer()
	for {
		switch ch := ReadSingleKey(); ch {
		case 'y', 'Y', 13:
			cfg.AutoUpdate = 1
		case 'n', 'N':
			cfg.AutoUpdate = 0
		default:
			continue
		}
		break
	}
	fmt.Println()
	if cfg.AutoUpdate == 1 {
		fmt.Printf("  %s\n", T("update.first_on"))
	} else {
		fmt.Printf("  %s\n", T("update.first_off"))
	}
}

// --- qc -u -----------------------------------------------------------------

// RunSelfUpdate updates qc.exe in place and returns the exit code.
// elevated is true in the copy started to get write access.
func RunSelfUpdate(elevated bool) int {
	fmt.Println()
	exePath, err := os.Executable()
	if err == nil {
		exePath, err = filepath.EvalSymlinks(exePath)
	}
	if err != nil {
		printStepFail(Tf("update.fail", err))
		return 1
	}

	var rel releaseInfo
	err = spinStep(T("update.checking"), func(set func(string)) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var e error
		rel, e = latestForUpdate(ctx)
		if e == nil {
			set(Tf("update.latest", rel.version(), Version))
		}
		return e
	})
	if err != nil {
		printStepFail(Tf("update.net_fail", friendlyError(err)))
		fmt.Println()
		return 1
	}
	writeUpdateCache(updateCache{LastCheck: time.Now(), Latest: rel.version()})

	if !newerVersion(rel.version(), Version) {
		printStepOK(Tf("update.up_to_date", Version))
		fmt.Println()
		return 0
	}

	asset, ok := findAsset(rel)
	if !ok {
		printStepFail(Tf("update.no_asset", rel.version(), runtime.GOARCH))
		return 1
	}
	wantHash, ok := strings.CutPrefix(strings.ToLower(asset.Digest), "sha256:")
	if !ok || len(wantHash) != 64 || asset.Size > maxDownloadSize {
		printStepFail(Tf("update.no_digest", rel.version(), Repo+"/releases"))
		return 1
	}

	dir := filepath.Dir(exePath)
	if !dirWritable(dir) {
		if elevated || IsAdmin() {
			printStepFail(Tf("update.fail", Tf("update.needs_admin", dir)))
			return 1
		}
		fmt.Printf("  %s\n", Tf("update.needs_admin", dir))
		return RunElevatedInTerminal([]string{"-u"})
	}

	newPath := exePath + ".new"
	_ = os.Remove(newPath)
	fail := func(e error) int {
		_ = os.Remove(newPath)
		printStepFail(Tf("update.fail", friendlyError(e)))
		fmt.Printf("  %s\n\n", T("update.unchanged"))
		return 1
	}

	var gotHash string
	err = spinStep(Tf("update.downloading", asset.Name), func(set func(string)) error {
		var e error
		gotHash, e = downloadFile(asset, newPath, set)
		return e
	})
	if err != nil {
		return fail(err)
	}

	err = spinStep(T("update.verifying"), func(func(string)) error {
		if gotHash != wantHash {
			return errors.New(T("update.bad_hash"))
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}

	err = spinStep(T("update.testing"), func(func(string)) error {
		return testBinary(newPath, rel.version())
	})
	if err != nil {
		return fail(err)
	}

	err = spinStep(T("update.installing"), func(func(string)) error {
		return swapBinary(exePath, newPath)
	})
	if err != nil {
		return fail(err)
	}

	fmt.Println()
	printStepOK(Tf("update.done", Version, rel.version()))
	if notes := releaseNotes(rel.Body, 8); notes != "" {
		fmt.Printf("\n  %s\n%s\n", T("update.whats_new"), notes)
	}
	fmt.Println()
	return 0
}

func findAsset(rel releaseInfo) (releaseAsset, bool) {
	for _, want := range assetNames() {
		for _, a := range rel.Assets {
			if strings.EqualFold(a.Name, want) {
				return a, true
			}
		}
	}
	return releaseAsset{}, false
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".qc-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

// downloadFile saves the asset to path and returns its SHA-256. The size is
// checked against the release, so a truncated or padded file is rejected.
func downloadFile(a releaseAsset, path string, set func(string)) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return "", err
	}

	// Without a size from the API, the server's Content-Length is used;
	// the checksum still decides whether the file is accepted.
	want := a.Size
	if want <= 0 {
		want = resp.ContentLength
	}
	if want > maxDownloadSize {
		return "", fmt.Errorf("download is larger than %s", FormatBytesHuman(maxDownloadSize))
	}
	limit := int64(maxDownloadSize)
	if want > 0 {
		limit = want
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return "", err
	}
	body := newStallReader(resp.Body, stallTimeout, cancel)
	defer body.stop()
	h := sha256.New()
	pw := &progressWriter{total: want, set: set}
	n, copyErr := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(body, limit+1))
	closeErr := f.Close()
	switch {
	case body.stalled():
		return "", errStalled
	case copyErr != nil:
		return "", copyErr
	case closeErr != nil:
		return "", closeErr
	case want > 0 && n < want:
		return "", errShort
	case n > limit:
		return "", fmt.Errorf("got more than the expected %d bytes", limit)
	}
	set(FormatBytesHuman(uint64(n)))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stallReader cancels the request when no data arrives for the idle
// window, so a dropped Wi-Fi link fails cleanly instead of hanging.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
	fired atomic.Bool
}

func newStallReader(r io.Reader, idle time.Duration, cancel func()) *stallReader {
	s := &stallReader{r: r, idle: idle}
	s.timer = time.AfterFunc(idle, func() {
		s.fired.Store(true)
		cancel()
	})
	return s
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(s.idle)
	}
	return n, err
}

func (s *stallReader) stop()         { s.timer.Stop() }
func (s *stallReader) stalled() bool { return s.fired.Load() }

type progressWriter struct {
	done, total int64
	set         func(string)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.total > 0 {
		p.set(fmt.Sprintf("%3d%%  %s / %s", p.done*100/p.total,
			FormatBytesHuman(uint64(p.done)), FormatBytesHuman(uint64(p.total))))
	} else {
		p.set(FormatBytesHuman(uint64(p.done)))
	}
	return len(b), nil
}

// testBinary runs the new file with --version in its own hidden console and
// checks it reports the expected version.
func testBinary(path, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("new version did not start: %v", err)
	}
	if !strings.Contains(string(out), " v"+want) {
		return fmt.Errorf("new version did not report v%s", want)
	}
	return nil
}

// swapBinary renames the running exe aside (Windows allows this) and moves
// the new one into place, putting the old one back if that fails. The old
// file is removed on the next start.
func swapBinary(exePath, newPath string) error {
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	old := exePath + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exePath, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		if rerr := os.Rename(old, exePath); rerr != nil {
			return fmt.Errorf("%v (restore failed: %v; your previous version is %s)", err, rerr, old)
		}
		return err
	}
	return nil
}

// RemoveOldBinary deletes the exe left behind by the last update.
func RemoveOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// releaseNotes returns up to maxLines of the release body, indented.
func releaseNotes(body string, maxLines int) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if len(lines) == maxLines {
			lines = append(lines, "    ...")
			break
		}
		lines = append(lines, "    "+l)
	}
	return strings.Join(lines, "\n")
}

// --- Step lines with a spinner ---------------------------------------------

// spinStep shows "⠋ label  detail" while fn runs, then a ✓ or ! line.
func spinStep(label string, fn func(set func(string)) error) error {
	if !stdoutSupportsVT() {
		err := fn(func(string) {})
		if err == nil {
			fmt.Printf("  %s\n", label)
		}
		return err
	}

	g := pickGlyphs()
	detail := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("%v", r)
			}
		}()
		done <- fn(func(s string) {
			select {
			case <-detail:
			default:
			}
			detail <- s
		})
	}()

	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")
	cur := ""
	t := time.NewTicker(frameEvery)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case s := <-detail:
			cur = s
		case err := <-done:
			select {
			case s := <-detail:
				cur = s
			default:
			}
			if err != nil {
				fmt.Printf("\r  %s%s%s  %s\x1b[K\n", YELLOW, g.warn, RST, label)
				return err
			}
			fmt.Printf("\r  %s%s%s  %s%s\x1b[K\n", GREEN, g.check, RST, label, dimDetail(cur))
			return nil
		case <-t.C:
		}
		fmt.Printf("\r  %s%s%s  %s%s\x1b[K", RED, g.spin[i%len(g.spin)], RST, label, dimDetail(cur))
	}
}

func dimDetail(s string) string {
	if s == "" {
		return ""
	}
	return "  " + DIM + s + RST
}

func printStepOK(msg string) {
	fmt.Printf("  %s%s%s  %s\n", GREEN, pickGlyphs().check, RST, msg)
}

func printStepFail(msg string) {
	fmt.Printf("  %s%s%s  %s\n", YELLOW, pickGlyphs().warn, RST, msg)
}
