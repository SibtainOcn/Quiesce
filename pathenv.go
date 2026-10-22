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

// `qc env` adds the folder holding qc.exe to the user PATH (HKCU), which
// needs no Admin, then tells Windows so new terminals see it.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	userEnvKey   = `Environment`
	systemEnvKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	maxPathLen   = 32767
)

var (
	errTempDir  = errors.New("temporary folder")
	errPathLong = errors.New("PATH too long")

	procSendMessageTimeoutW = moduser32.NewProc("SendMessageTimeoutW")
)

type pathResult int

const (
	pathAdded pathResult = iota
	pathAlready
)

// normDir makes PATH entries comparable: env vars expanded, cleaned,
// no trailing backslash, lower case.
func normDir(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if s == "" {
		return ""
	}
	if exp, err := registry.ExpandString(s); err == nil {
		s = exp
	}
	return strings.ToLower(strings.TrimRight(filepath.Clean(s), `\`))
}

func pathEntries(list string) []string {
	var out []string
	for _, e := range strings.Split(list, ";") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func pathHas(list, dir string) bool {
	want := normDir(dir)
	for _, e := range pathEntries(list) {
		if normDir(e) == want {
			return true
		}
	}
	return false
}

func appendPath(list, dir string) string {
	list = strings.TrimRight(strings.TrimSpace(list), ";")
	if list == "" {
		return dir
	}
	return list + ";" + dir
}

func readPathValue(root registry.Key, key string) (string, uint32) {
	k, err := registry.OpenKey(root, key, registry.QUERY_VALUE)
	if err != nil {
		return "", registry.EXPAND_SZ
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if err != nil {
		return "", registry.EXPAND_SZ
	}
	return v, typ
}

// inTempDir is true for folders that get cleaned, Quiesce's own User and
// Windows Temp steps included - a PATH entry there would break later.
func inTempDir(dir string) bool {
	d := normDir(dir)
	for _, t := range []string{os.TempDir(), os.Getenv("TEMP"), os.Getenv("TMP"),
		filepath.Join(getEnvOrDefault("SystemRoot", `C:\Windows`), "Temp")} {
		if t = normDir(t); t != "" && (d == t || strings.HasPrefix(d, t+`\`)) {
			return true
		}
	}
	return false
}

// addToUserPath puts dir on the user PATH unless it is already on the user
// or system PATH.
func addToUserPath(dir string) (pathResult, error) {
	res, err := addToPathKey(registry.CURRENT_USER, userEnvKey, dir)
	if err == nil && res == pathAdded {
		broadcastEnvChange()
	}
	return res, err
}

// addToPathKey does the work on any key, so tests can use a scratch one.
func addToPathKey(root registry.Key, key, dir string) (pathResult, error) {
	sys, _ := readPathValue(registry.LOCAL_MACHINE, systemEnvKey)
	user, typ := readPathValue(root, key)
	if pathHas(user, dir) || pathHas(sys, dir) {
		return pathAlready, nil
	}
	if inTempDir(dir) {
		return 0, errTempDir
	}
	updated := appendPath(user, dir)
	if len(updated) > maxPathLen {
		return 0, errPathLong
	}

	k, _, err := registry.CreateKey(root, key, registry.SET_VALUE)
	if err != nil {
		return 0, err
	}
	defer k.Close()
	if typ == registry.SZ && !strings.Contains(updated, "%") {
		err = k.SetStringValue("Path", updated)
	} else {
		err = k.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return 0, err
	}
	return pathAdded, nil
}

// broadcastEnvChange tells Explorer and other apps the environment changed,
// so terminals opened from now on get the new PATH.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	if procSendMessageTimeoutW.Find() != nil {
		return
	}
	env, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

// shadowingQc returns another folder whose qc.exe Windows would find before
// ours (system PATH is searched before user PATH), or "".
func shadowingQc(dir string) string {
	sys, _ := readPathValue(registry.LOCAL_MACHINE, systemEnvKey)
	user, _ := readPathValue(registry.CURRENT_USER, userEnvKey)
	own := normDir(dir)
	for _, e := range append(pathEntries(sys), pathEntries(user)...) {
		d := normDir(e)
		if d == own {
			return ""
		}
		if d == "" {
			continue
		}
		if fi, err := os.Stat(filepath.Join(d, "qc.exe")); err == nil && !fi.IsDir() {
			return e
		}
	}
	return ""
}

// RunPathCommand is `qc env`.
func RunPathCommand() int {
	fmt.Println()
	code := addToPathAndReport()
	fmt.Println()
	return code
}

// installTarget decides where `qc` should live. A qc.exe in a permanent
// folder stays put; a download (any other name, or in Downloads, Desktop or
// a temp folder) is copied to the per-user programs folder as qc.exe,
// the way installers do, so nobody has to rename or move it.
func installTarget() (exe, dir string, install bool, err error) {
	exe, err = os.Executable()
	if err != nil {
		return "", "", false, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir = filepath.Dir(exe)
	if strings.EqualFold(filepath.Base(exe), "qc.exe") && !inTempDir(dir) && !inDropFolder(dir) {
		return exe, dir, false, nil
	}
	inst, err := installDir()
	return exe, inst, true, err
}

// installDir is %LOCALAPPDATA%\Programs\Quiesce: per user, no Admin needed.
func installDir() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil || base == "" {
		if base = os.Getenv("LOCALAPPDATA"); base == "" {
			return "", errors.New("local app data folder not found")
		}
	}
	return filepath.Join(base, "Programs", AppName), nil
}

// inDropFolder is true for Downloads and Desktop, where downloads land.
func inDropFolder(dir string) bool {
	d := normDir(dir)
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Downloads, windows.FOLDERID_Desktop} {
		p, err := windows.KnownFolderPath(id, 0)
		if p = normDir(p); err == nil && p != "" && (d == p || strings.HasPrefix(d, p+`\`)) {
			return true
		}
	}
	return false
}

// installCopy puts src into dir as qc.exe, replacing an older copy safely,
// and brings the settings file along when the target has none.
func installCopy(src, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	target := filepath.Join(dir, "qc.exe")
	if si, err := os.Stat(src); err == nil {
		if ti, err := os.Stat(target); err == nil && os.SameFile(si, ti) {
			return nil
		}
	}
	if same, _ := sameContent(src, target); !same {
		tmp := target + ".new"
		_ = os.Remove(tmp)
		if err := copyFile(src, tmp); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if _, err := os.Stat(target); err == nil {
			if err := swapBinary(target, tmp); err != nil {
				_ = os.Remove(tmp)
				return err
			}
		} else if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}

	cfgSrc := filepath.Join(filepath.Dir(src), "cleaner_config.dat")
	cfgDst := filepath.Join(dir, "cleaner_config.dat")
	if _, err := os.Stat(cfgDst); os.IsNotExist(err) {
		if _, err := os.Stat(cfgSrc); err == nil {
			_ = copyFile(cfgSrc, cfgDst)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameContent(a, b string) (bool, error) {
	ha, err := fileHash(a)
	if err != nil {
		return false, err
	}
	hb, err := fileHash(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func addToPathAndReport() int {
	exe, dir, install, err := installTarget()
	if err != nil {
		printStepFail(Tf("env.fail", err))
		return 1
	}
	if install {
		if err := installCopy(exe, dir); err != nil {
			printStepFail(Tf("env.install_fail", err))
			return 1
		}
		printStepOK(Tf("env.installed", filepath.Join(dir, "qc.exe")))
	}
	res, err := addToUserPath(dir)
	switch {
	case errors.Is(err, errTempDir):
		printStepFail(Tf("env.temp_dir", dir, AppShort))
		return 1
	case errors.Is(err, errPathLong):
		printStepFail(T("env.too_long"))
		return 1
	case err != nil:
		printStepFail(Tf("env.fail", err))
		return 1
	case res == pathAlready:
		printStepOK(Tf("env.already", dir))
	default:
		printStepOK(Tf("env.added", dir))
		fmt.Printf("\n  %s\n\n", T("env.next"))
		printPathExamples()
	}
	if other := shadowingQc(dir); other != "" {
		fmt.Printf("\n  %s%s%s\n", YELLOW, Tf("env.other", other), RST)
	}
	if install && normDir(filepath.Dir(exe)) != normDir(dir) && !inTempDir(filepath.Dir(exe)) {
		fmt.Printf("\n  %s%s%s\n", DIM, Tf("env.delete_download", exe), RST)
	}
	return 0
}

func printPathExamples() {
	ex := [][2]string{
		{"qc", T("env.ex_menu")},
		{"qc all", T("env.ex_all")},
		{"qc temp ram", T("env.ex_temp_ram")},
		{"qc -u", T("env.ex_update")},
		{"qc --help", T("env.ex_help")},
	}
	for _, e := range ex {
		fmt.Printf("    %s%-14s%s %s%s%s\n", WHITE, e[0], RST, DIM, e[1], RST)
	}
}

// RunRepoCommand is `qc repo` / `qc github`: open the project page in the
// default browser, or print the link if that fails.
func RunRepoCommand() int {
	fmt.Println()
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(Repo)
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		printStepFail(Tf("repo.fail", Repo))
		fmt.Println()
		return 1
	}
	printStepOK(Tf("repo.opened", Repo))
	fmt.Println()
	return 0
}

// OfferAddToPath is the first-run question; it is skipped when Quiesce is
// already on PATH or sits in a temporary folder.
func OfferAddToPath() {
	_, dir, install, err := installTarget()
	if err != nil {
		return
	}
	sys, _ := readPathValue(registry.LOCAL_MACHINE, systemEnvKey)
	user, _ := readPathValue(registry.CURRENT_USER, userEnvKey)
	if !install && (pathHas(user, dir) || pathHas(sys, dir)) {
		return
	}

	question := T("env.first_run")
	if install {
		question = T("env.first_install")
	}
	fmt.Printf("\n  %s ", question)
	FlushKeyBuffer()
	yes := false
	for {
		switch ReadSingleKey() {
		case 'y', 'Y', 13:
			yes = true
		case 'n', 'N':
		default:
			continue
		}
		break
	}
	fmt.Println()
	if !yes {
		fmt.Printf("  %s\n", Tf("env.skipped", AppShort))
		return
	}
	addToPathAndReport()
}
