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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	latestReleaseAPI = "https://api.github.com/repos/SibtainOcn/Quiesce/releases/latest"
	updateCheckEvery = 24 * time.Hour
	maxDownloadSize  = 64 << 20
	createNoWindow   = 0x08000000
)

var errNoChecksum = errors.New("no checksum")

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

// assetName is the release file for this build.
func assetName() string {
	if runtime.GOARCH == "386" {
		return "qc-386.exe"
	}
	return "qc.exe"
}

var httpClient = &http.Client{Timeout: 2 * time.Minute}

func fetchLatestRelease(ctx context.Context) (releaseInfo, error) {
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
	if resp.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return rel, err
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
		if rel, err := fetchLatestRelease(ctx); err == nil {
			c.Latest = rel.version()
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
		rel, e = fetchLatestRelease(ctx)
		if e == nil {
			set(Tf("update.latest", rel.version(), Version))
		}
		return e
	})
	if err != nil {
		printStepFail(Tf("update.net_fail", err))
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
	if !ok || len(wantHash) != 64 || asset.Size <= 0 || asset.Size > maxDownloadSize {
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
		printStepFail(Tf("update.fail", e))
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
	want := assetName()
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, want) {
			return a, true
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
	req, err := http.NewRequest(http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pw := &progressWriter{total: a.Size, set: set}
	n, copyErr := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, a.Size+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		return "", copyErr
	case closeErr != nil:
		return "", closeErr
	case n != a.Size:
		return "", fmt.Errorf("got %d bytes, expected %d", n, a.Size)
	}
	set(FormatBytesHuman(uint64(n)))
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressWriter struct {
	done, total int64
	set         func(string)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	p.set(fmt.Sprintf("%3d%%  %s / %s", p.done*100/max(p.total, 1),
		FormatBytesHuman(uint64(p.done)), FormatBytesHuman(uint64(p.total))))
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
