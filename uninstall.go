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

// `qc uninstall` closes other Quiesce windows, deletes Quiesce's files and
// takes its folder off the user PATH. A running exe can't delete itself, so
// a hidden cmd removes qc.exe once this process has exited.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	errNeedsAdmin = errors.New("needs administrator")
	errPathChars  = errors.New("unsupported characters in path")
)

// qcDataFiles live next to qc.exe.
var qcDataFiles = []string{"cleaner_config.dat", "qc_update.dat", "qc_update.dat.tmp"}

func isUninstallCmd(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "uninstall" || s == "--uninstall"
}

// parseUninstallArgs reads the words after "uninstall": only -y is allowed.
func parseUninstallArgs(args []string) (yes bool, err error) {
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "-y", "--yes", "/y":
			yes = true
		case "":
		default:
			return false, errors.New(Tf("cli.unknown", a))
		}
	}
	return yes, nil
}

// qcFolder is one folder holding Quiesce files.
type qcFolder struct {
	dir      string
	exe      string
	files    []string // Quiesce's files that exist here, exe included
	logsDir  string
	onlyOurs bool // the folder holds nothing else
}

func scanQcFolder(dir, exeName string) qcFolder {
	f := qcFolder{dir: dir, exe: filepath.Join(dir, exeName), logsDir: filepath.Join(dir, "logs")}
	names := append([]string{exeName, exeName + ".new", exeName + ".old"}, qcDataFiles...)
	for _, n := range names {
		if p := filepath.Join(dir, n); isRegularFile(p) {
			f.files = append(f.files, p)
		}
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".qc-write-") && e.Type().IsRegular() {
				f.files = append(f.files, filepath.Join(dir, e.Name()))
			}
		}
	}
	if entries, err := os.ReadDir(f.logsDir); err == nil {
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if e.Type().IsRegular() && strings.HasPrefix(n, "quiesce_") && strings.HasSuffix(n, ".log") {
				f.files = append(f.files, filepath.Join(f.logsDir, e.Name()))
			}
		}
	}
	f.onlyOurs = onlyOurFiles(dir, f.files)
	return f
}

func isRegularFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// onlyOurFiles reports whether dir (and its logs folder) holds nothing but files.
func onlyOurFiles(dir string, files []string) bool {
	ours := map[string]bool{}
	for _, f := range files {
		ours[strings.ToLower(filepath.Clean(f))] = true
	}
	var check func(d string, allowLogs bool) bool
	check = func(d string, allowLogs bool) bool {
		entries, err := os.ReadDir(d)
		if err != nil {
			return false
		}
		for _, e := range entries {
			p := filepath.Join(d, e.Name())
			if e.IsDir() {
				if allowLogs && strings.EqualFold(e.Name(), "logs") && check(p, false) {
					continue
				}
				return false
			}
			if !ours[strings.ToLower(p)] {
				return false
			}
		}
		return true
	}
	return check(dir, true)
}

type uninstallPlan struct {
	self     string
	folders  []qcFolder
	pathDirs []string // folders to take off the user PATH
	rmdir    string   // the install folder, removed once empty, or ""
}

func (p uninstallPlan) exes() []string {
	var out []string
	for _, f := range p.folders {
		out = append(out, f.exe)
	}
	return out
}

// planUninstall covers the running exe's folder and the install folder.
// A folder of the user's own (C:\Tools) only loses Quiesce's files; it is
// taken off PATH only when nothing else is left in it.
func planUninstall() (uninstallPlan, error) {
	var p uninstallPlan
	self, err := os.Executable()
	if err != nil {
		return p, err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	p.self = self
	selfDir := filepath.Dir(self)
	own := scanQcFolder(selfDir, filepath.Base(self))
	p.folders = append(p.folders, own)

	inst, instErr := installDir()
	if instErr == nil {
		p.pathDirs = append(p.pathDirs, inst)
		if fi, err := os.Stat(inst); err == nil && fi.IsDir() {
			p.rmdir = inst
			if normDir(inst) != normDir(selfDir) {
				p.folders = append(p.folders, scanQcFolder(inst, "qc.exe"))
			}
		}
	}
	if (instErr != nil || normDir(inst) != normDir(selfDir)) && own.onlyOurs &&
		!inTempDir(selfDir) && !inDropFolder(selfDir) {
		p.pathDirs = append(p.pathDirs, selfDir)
	}
	return p, nil
}

// otherQcProcesses finds running copies of exes, this process excluded.
func otherQcProcesses(exes []string) []uint32 {
	want := map[string]bool{}
	names := map[string]bool{}
	for _, e := range exes {
		want[strings.ToLower(filepath.Clean(e))] = true
		names[strings.ToLower(filepath.Base(e))] = true
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	me := uint32(os.Getpid())
	var out []uint32
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if pe.ProcessID == me || !names[strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))] {
			continue
		}
		if p, ok := processImage(pe.ProcessID); ok && want[strings.ToLower(filepath.Clean(p))] {
			out = append(out, pe.ProcessID)
		}
	}
	return out
}

func processImage(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

// closeProcesses ends every pid, or none: an Admin window this process
// can't close stops the uninstall before anything is touched.
func closeProcesses(pids []uint32) error {
	var handles []windows.Handle
	defer func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}()
	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return errNeedsAdmin
		}
		if err != nil {
			continue // already gone
		}
		handles = append(handles, h)
	}
	for _, h := range handles {
		if err := windows.TerminateProcess(h, 1); err != nil {
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				return errNeedsAdmin
			}
			continue
		}
		windows.WaitForSingleObject(h, 5000)
	}
	return nil
}

// removeFiles deletes files except self, retrying briefly while a just
// closed process lets go of them. It returns what it couldn't delete.
func removeFiles(files []string, self string) (left []string, lastErr error) {
	pending := []string{}
	for _, f := range files {
		if !strings.EqualFold(filepath.Clean(f), filepath.Clean(self)) {
			pending = append(pending, f)
		}
	}
	for try := 0; try < 10 && len(pending) > 0; try++ {
		if try > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		var next []string
		for _, f := range pending {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				next = append(next, f)
				lastErr = err
			}
		}
		pending = next
	}
	return pending, lastErr
}

// removeFromPathKey takes every entry matching dirs off the Path value and
// keeps the rest as they were. It reports whether anything changed.
func removeFromPathKey(root registry.Key, key string, dirs []string) (bool, error) {
	cur, typ := readPathValue(root, key)
	if cur == "" {
		return false, nil
	}
	drop := map[string]bool{}
	for _, d := range dirs {
		if n := normDir(d); n != "" {
			drop[n] = true
		}
	}
	var kept []string
	removed := false
	for _, e := range pathEntries(cur) {
		if drop[normDir(e)] {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return false, nil
	}

	k, err := registry.OpenKey(root, key, registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	updated := strings.Join(kept, ";")
	switch {
	case updated == "":
		err = k.DeleteValue("Path")
	case typ == registry.SZ:
		err = k.SetStringValue("Path", updated)
	default:
		err = k.SetExpandStringValue("Path", updated)
	}
	return err == nil, err
}

// selfDeleteScript is the cmd line that deletes exe after this process
// exits (retrying each second for up to 10 minutes), then removes dir if
// it is empty.
func selfDeleteScript(exe, dir string) (string, error) {
	if strings.ContainsAny(exe+dir, `%"`) {
		return "", errPathChars
	}
	q := `"` + exe + `"`
	s := fmt.Sprintf(`for /l %%i in (1,1,600) do if exist %s (del /f /q %s >nul 2>&1 & if exist %s %%SystemRoot%%\System32\ping.exe -n 2 127.0.0.1 >nul)`, q, q, q)
	if dir != "" {
		s += ` & rmdir "` + dir + `" >nul 2>&1`
	}
	return s, nil
}

func scheduleSelfDelete(exe, dir string) error {
	script, err := selfDeleteScript(exe, dir)
	if err != nil {
		return err
	}
	sysDir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	start := func(flags uint32) error {
		c := exec.Command(filepath.Join(sysDir, "cmd.exe"))
		c.Dir = sysDir
		c.SysProcAttr = &syscall.SysProcAttr{
			CmdLine:       `cmd.exe /d /q /v:off /s /c "` + script + `"`,
			CreationFlags: flags,
			HideWindow:    true,
		}
		if err := c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	base := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP)
	if err := start(base | windows.CREATE_BREAKAWAY_FROM_JOB); err == nil {
		return nil
	}
	return start(base)
}

// askUninstall is the one confirmation; ENTER, N, Esc and Ctrl+C all say no.
func askUninstall() bool {
	fmt.Printf("  %s ", T("uninstall.confirm"))
	FlushKeyBuffer()
	for {
		switch ReadSingleKey() {
		case 'y', 'Y':
			fmt.Println("y")
			return true
		case 'n', 'N', 13, 27, 3:
			fmt.Println("n")
			return false
		}
	}
}

// RunUninstall is `qc uninstall [-y]`.
func RunUninstall(args []string) int {
	fmt.Println()
	yes, err := parseUninstallArgs(args)
	if err != nil {
		fmt.Printf("  \x1b[31m%s\x1b[0m\n  %s\n\n", err, Tf("cli.see_help", AppShort))
		return 2
	}
	plan, err := planUninstall()
	if err != nil {
		printStepFail(Tf("uninstall.fail", err))
		fmt.Println()
		return 1
	}
	pids := otherQcProcesses(plan.exes())

	if !yes {
		if !stdinIsConsole() {
			printStepFail(Tf("uninstall.need_yes", AppShort))
			fmt.Println()
			return 2
		}
		if len(pids) > 0 {
			fmt.Printf("  %s%s%s\n", YELLOW, Tf("uninstall.running", len(pids)), RST)
		}
		if !askUninstall() {
			fmt.Printf("  %s\n\n", T("uninstall.cancelled"))
			return 1
		}
		fmt.Println()
	}

	if len(pids) > 0 {
		err := spinStep(Tf("uninstall.closing", len(pids)), func(func(string)) error {
			return closeProcesses(pids)
		})
		if err != nil {
			if errors.Is(err, errNeedsAdmin) {
				printStepFail(Tf("uninstall.close_admin", AppShort))
			} else {
				printStepFail(Tf("uninstall.fail", err))
			}
			fmt.Printf("  %s\n\n", T("uninstall.unchanged"))
			return 1
		}
	}

	code := 0
	var files []string
	for _, f := range plan.folders {
		files = append(files, f.files...)
	}

	// Settings, logs and any other qc.exe.
	var left []string
	var rmErr error
	_ = spinStep(T("uninstall.removing"), func(set func(string)) error {
		set(Tf("uninstall.n_files", len(files)))
		left, rmErr = removeFiles(files, plan.self)
		for _, f := range plan.folders {
			_ = os.Remove(f.logsDir)
		}
		if len(left) > 0 {
			return rmErr
		}
		return nil
	})
	if len(left) > 0 {
		code = 1
		msg := T("uninstall.locked")
		if errors.Is(rmErr, os.ErrPermission) && !IsAdmin() {
			msg = Tf("uninstall.locked_admin", AppShort)
		}
		printStepFail(msg)
		for _, f := range left {
			fmt.Printf("     %s%s%s\n", DIM, f, RST)
		}
	}

	// The running qc.exe can be moved but not deleted: it goes to Temp, and
	// the helper deletes it there after exit.
	parked := plan.self
	dirLeft := false
	_ = spinStep(Tf("uninstall.removing_exe", filepath.Base(plan.self)), func(set func(string)) error {
		if p, e := moveOutSelf(plan.self); e == nil {
			parked = p
			set(filepath.Dir(plan.self))
		} else {
			set(T("uninstall.exe_later"))
		}
		return nil
	})

	if plan.rmdir != "" && parked != plan.self {
		_ = spinStep(T("uninstall.removing_dir"), func(set func(string)) error {
			err := os.Remove(plan.rmdir)
			switch {
			case err == nil, os.IsNotExist(err):
				set(plan.rmdir)
			case errors.Is(err, windows.ERROR_DIR_NOT_EMPTY):
				set(T("uninstall.dir_kept"))
			default:
				// In use, e.g. a terminal open in it: the helper retries.
				dirLeft = true
				set(T("uninstall.dir_later"))
			}
			return nil
		})
	}

	var removed bool
	err = spinStep(T("uninstall.path"), func(set func(string)) error {
		var e error
		removed, e = removeFromPathKey(registry.CURRENT_USER, userEnvKey, plan.pathDirs)
		if removed {
			broadcastEnvChange()
			set(T("uninstall.path_removed"))
		} else if e == nil {
			set(T("uninstall.path_none"))
		}
		return e
	})
	if err != nil {
		code = 1
		printStepFail(Tf("env.fail", err))
	}
	sys, _ := readPathValue(registry.LOCAL_MACHINE, systemEnvKey)
	for _, d := range plan.pathDirs {
		if pathHas(sys, d) {
			fmt.Printf("  %s%s%s\n", YELLOW, Tf("uninstall.system_path", d), RST)
		}
	}

	// The helper also removes the folder when the move failed or it was busy.
	helperDir := ""
	if parked == plan.self || dirLeft {
		helperDir = plan.rmdir
	}
	helperErr := scheduleSelfDelete(parked, helperDir)
	// Backup for Admin: Windows deletes the file at the next restart.
	if IsAdmin() {
		if p, err := windows.UTF16PtrFromString(parked); err == nil {
			_ = windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
		}
	}
	if helperErr != nil && parked == plan.self {
		code = 1
		printStepFail(Tf("uninstall.self_fail", plan.self))
	}

	fmt.Println()
	if code == 0 {
		printStepOK(T("uninstall.done"))
	} else {
		printStepFail(T("uninstall.partial"))
	}
	fmt.Printf("  %s%s%s\n\n", DIM, T("uninstall.reinstall"), RST)

	// -y is for scripts: never wait on a key there.
	if !yes && ownsConsoleWindow() {
		fmt.Print(T("common.press_enter"))
		WaitForEnter()
	}
	return code
}

// moveOutSelf moves the running exe into Temp and returns its new path.
func moveOutSelf(self string) (string, error) {
	dst := filepath.Join(os.TempDir(), fmt.Sprintf("qc-uninstall-%d-%d.exe", os.Getpid(), time.Now().UnixNano()))
	if err := os.Rename(self, dst); err != nil {
		return "", err
	}
	return dst, nil
}
