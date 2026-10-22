package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestUninstallArgs(t *testing.T) {
	for _, s := range []string{"uninstall", "UNINSTALL", " --uninstall "} {
		if !isUninstallCmd(s) {
			t.Errorf("%q not recognised", s)
		}
	}
	if isUninstallCmd("remove") || isUninstallCmd("un") {
		t.Error("false match")
	}
	if yes, err := parseUninstallArgs(nil); yes || err != nil {
		t.Errorf("no args: %v %v", yes, err)
	}
	for _, a := range []string{"-y", "--YES", "/y"} {
		if yes, err := parseUninstallArgs([]string{a}); !yes || err != nil {
			t.Errorf("%q: %v %v", a, yes, err)
		}
	}
	if _, err := parseUninstallArgs([]string{"-y", "temp"}); err == nil {
		t.Error("unknown word accepted")
	}
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(dir, n)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanQcFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Quiesce [1]")
	writeFiles(t, dir, "qc.exe", "qc.exe.old", "cleaner_config.dat", "qc_update.dat",
		".qc-write-123", `logs\quiesce_2026-10-22_101010.log`)

	f := scanQcFolder(dir, "qc.exe")
	if len(f.files) != 6 {
		t.Errorf("found %d files: %v", len(f.files), f.files)
	}
	if !f.onlyOurs {
		t.Error("folder with only Quiesce files not recognised")
	}

	writeFiles(t, dir, `logs\notes.txt`)
	if scanQcFolder(dir, "qc.exe").onlyOurs {
		t.Error("foreign file in logs ignored")
	}
	_ = os.Remove(filepath.Join(dir, "logs", "notes.txt"))
	writeFiles(t, dir, "other-tool.exe")
	f = scanQcFolder(dir, "qc.exe")
	if f.onlyOurs {
		t.Error("foreign file ignored")
	}
	for _, p := range f.files {
		if strings.Contains(p, "other-tool") {
			t.Error("foreign file would be deleted")
		}
	}
}

func TestRemoveFilesSkipsSelf(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "qc.exe", "cleaner_config.dat", "qc_update.dat")
	self := filepath.Join(dir, "qc.exe")
	files := scanQcFolder(dir, "qc.exe").files
	files = append(files, filepath.Join(dir, "already-gone.dat"))

	left, err := removeFiles(files, strings.ToUpper(self))
	if len(left) != 0 || err != nil {
		t.Fatalf("left %v, %v", left, err)
	}
	if !isRegularFile(self) {
		t.Error("running exe deleted")
	}
	if isRegularFile(filepath.Join(dir, "cleaner_config.dat")) {
		t.Error("config kept")
	}
}

func TestRemoveFromPathKey(t *testing.T) {
	const key = `Software\QuiesceTest\UninstallEnv`
	defer func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\QuiesceTest`)
	}()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		t.Skip("cannot create test key:", err)
	}
	_ = k.SetExpandStringValue("Path", `%USERPROFILE%\bin;C:\Programs\Quiesce\;C:\Other;c:\programs\quiesce`)
	k.Close()

	removed, err := removeFromPathKey(registry.CURRENT_USER, key, []string{`C:\Programs\Quiesce`})
	if !removed || err != nil {
		t.Fatalf("remove: %v %v", removed, err)
	}
	got, typ := readPathValue(registry.CURRENT_USER, key)
	if got != `%USERPROFILE%\bin;C:\Other` || typ != registry.EXPAND_SZ {
		t.Errorf("value %q type %d", got, typ)
	}

	if removed, err := removeFromPathKey(registry.CURRENT_USER, key, []string{`C:\Programs\Quiesce`}); removed || err != nil {
		t.Errorf("second remove: %v %v", removed, err)
	}

	// Removing the last entry deletes the value instead of leaving "".
	if _, err := removeFromPathKey(registry.CURRENT_USER, key, []string{`%USERPROFILE%\bin`, `C:\Other\`}); err != nil {
		t.Fatal(err)
	}
	k, _ = registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	defer k.Close()
	if _, _, err := k.GetStringValue("Path"); err != registry.ErrNotExist {
		t.Errorf("empty Path value left: %v", err)
	}
}

func TestSelfDeleteScriptRejectsOddPaths(t *testing.T) {
	if _, err := selfDeleteScript(`C:\100%\qc.exe`, ""); err != errPathChars {
		t.Error("percent sign accepted")
	}
	s, err := selfDeleteScript(`C:\Program Files (x86)\Q & A\qc.exe`, `C:\Program Files (x86)\Q & A`)
	if err != nil || !strings.Contains(s, `rmdir "C:\Program Files (x86)\Q & A"`) {
		t.Errorf("%q %v", s, err)
	}
}

// The helper really deletes the exe and its folder, even with spaces,
// brackets and & in the path.
func TestScheduleSelfDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Q & A (x86) [1]")
	writeFiles(t, dir, "qc.exe")
	exe := filepath.Join(dir, "qc.exe")
	if err := scheduleSelfDelete(exe, dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("helper did not remove the exe and folder")
}

func TestMoveOutSelf(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "qc.exe")
	dst, err := moveOutSelf(filepath.Join(dir, "qc.exe"))
	if err != nil {
		t.Skip("temp on another drive:", err)
	}
	defer os.Remove(dst)
	if !isRegularFile(dst) || isRegularFile(filepath.Join(dir, "qc.exe")) {
		t.Error("not moved")
	}
}

// A running copy is found by its full path and closed.
func TestFindAndCloseRunningCopy(t *testing.T) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "qc.exe")
	b, err := os.ReadFile(filepath.Join(sys, "ping.exe"))
	if err != nil {
		t.Skip(err)
	}
	_ = os.WriteFile(fake, b, 0755)

	cmd := exec.Command(fake, "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start test process:", err)
	}
	defer cmd.Process.Kill()

	var pids []uint32
	for i := 0; i < 20 && len(pids) == 0; i++ {
		pids = otherQcProcesses([]string{strings.ToUpper(fake), filepath.Join(t.TempDir(), "qc.exe")})
		time.Sleep(50 * time.Millisecond)
	}
	if len(pids) != 1 || pids[0] != uint32(cmd.Process.Pid) {
		t.Fatalf("found %v, want %d", pids, cmd.Process.Pid)
	}
	if err := closeProcesses(pids); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if left, _ := removeFiles([]string{fake}, ""); len(left) != 0 {
		t.Error("exe still locked after closing")
	}
}
