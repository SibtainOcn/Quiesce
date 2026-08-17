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

import (
	"fmt"
	"math"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"quiesce/locales"
)

var (
	modntdll                   = windows.NewLazySystemDLL("ntdll.dll")
	modkernel32                = windows.NewLazySystemDLL("kernel32.dll")
	modpsapi                   = windows.NewLazySystemDLL("psapi.dll")
	moduser32                  = windows.NewLazySystemDLL("user32.dll")
	procNtSetSystemInformation = modntdll.NewProc("NtSetSystemInformation")
	procGlobalMemoryStatusEx   = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes         = modkernel32.NewProc("GetSystemTimes")
	procSetSystemFileCacheSize = modkernel32.NewProc("SetSystemFileCacheSize")
	procEmptyWorkingSet        = modpsapi.NewProc("EmptyWorkingSet")
	procGetForegroundWindow    = moduser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcId  = moduser32.NewProc("GetWindowThreadProcessId")
)

const (
	SystemMemoryListInformation = 80
	MemoryFlushModifiedList     = 3
	MemoryPurgeStandbyList      = 4
)

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type MemoryStats struct {
	TotalMB     uint64
	FreeMB      uint64
	UsedMB      uint64
	UsedPercent float64
}

// RamResult carries everything the end-of-run summary needs to report about
// step [10] - the totals plus which of the four sub-operations actually ran,
// so the summary can attribute the numbers instead of just showing an MB delta.
type RamResult struct {
	FreedMB      int64
	BeforePct    float64
	AfterPct     float64
	DropPct      float64
	OpsRun       []string // short labels of the sub-options that executed
	Trimmed      int      // processes trimmed (working-set trim only)
	TrimSkipped  int      // processes skipped (protected/no access)
	PrivFailed   bool     // could not enable SeProfileSingleProcessPrivilege
	NothingToRun bool     // all four sub-options were OFF
}

type SystemStats struct {
	CpuLoad uint64
	UsedGB  float64
	TotalGB float64
	Pct     uint64
}

// EnablePrivilege enables SeProfileSingleProcessPrivilege, which is what
// NtSetSystemInformation(SystemMemoryListInformation) requires.
func EnablePrivilege() bool {
	return enablePrivilege("SeProfileSingleProcessPrivilege")
}

// enablePrivilege enables a named privilege on the current process token.
func enablePrivilege(name string) bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	privName, _ := windows.UTF16PtrFromString(name)
	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, privName, &luid)
	if err != nil {
		return false
	}

	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{
				Luid:       luid,
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			},
		},
	}

	err = windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil)
	return err == nil
}

func FlushModifiedList() int32 {
	cmd := int32(MemoryFlushModifiedList)
	r1, _, _ := procNtSetSystemInformation.Call(
		uintptr(SystemMemoryListInformation),
		uintptr(unsafe.Pointer(&cmd)),
		unsafe.Sizeof(cmd),
	)
	return int32(r1)
}

func PurgeStandbyList() int32 {
	cmd := int32(MemoryPurgeStandbyList)
	r1, _, _ := procNtSetSystemInformation.Call(
		uintptr(SystemMemoryListInformation),
		uintptr(unsafe.Pointer(&cmd)),
		unsafe.Sizeof(cmd),
	)
	return int32(r1)
}

// PurgeSystemFileCache flushes the kernel's system file cache working set by
// calling SetSystemFileCacheSize with min/max set to (SIZE_T)-1, which is the
// documented "reset the cache" sentinel. This only drops cached file data -
// no process ever loses a page it is currently working on - so it is the safe
// half of what vendor "memory booster" tools do.
//
// Requires SeIncreaseQuotaPrivilege.
func PurgeSystemFileCache() bool {
	// LazyProc.Call panics if the export can't be resolved, so resolve first.
	if err := procSetSystemFileCacheSize.Find(); err != nil {
		return false
	}
	if !enablePrivilege("SeIncreaseQuotaPrivilege") {
		return false
	}
	minusOne := ^uintptr(0) // (SIZE_T)-1
	r1, _, _ := procSetSystemFileCacheSize.Call(minusOne, minusOne, 0)
	return r1 != 0
}

// protectedProcNames are processes we never trim. Trimming these either fails
// outright (they are protected) or hurts responsiveness far more than the
// memory it reclaims is worth.
var protectedProcNames = map[string]bool{
	"system":             true,
	"registry":           true,
	"memory compression": true,
	"smss.exe":           true,
	"csrss.exe":          true,
	"wininit.exe":        true,
	"winlogon.exe":       true,
	"services.exe":       true,
	"lsass.exe":          true,
	"msmpeng.exe":        true,
	"dwm.exe":            true,
}

// getForegroundPID returns the PID owning the current foreground window, or 0
// if there isn't one. That process is skipped during a working-set trim so the
// app the user is actually looking at doesn't get paged out from under them.
func getForegroundPID() uint32 {
	if procGetForegroundWindow.Find() != nil || procGetWindowThreadProcId.Find() != nil {
		return 0
	}
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// TrimAllWorkingSets calls EmptyWorkingSet on every accessible process, which
// forces its private working set out to the pagefile/standby list. This is the
// operation that makes vendor cleaners (ASUS, etc.) show a large drop in "in
// use" memory - but those pages were live, so the owning apps will hard-fault
// them straight back in on next use. Hence: opt-in only.
//
// Skipped: PID 0/4, our own process, the foreground window's process, and the
// protected system processes above.
//
// Returns (trimmed, skipped).
func TrimAllWorkingSets() (int, int) {
	if err := procEmptyWorkingSet.Find(); err != nil {
		return 0, 0
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, 0
	}
	defer windows.CloseHandle(snapshot)

	selfPID := uint32(windows.GetCurrentProcessId())
	fgPID := getForegroundPID()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return 0, 0
	}

	trimmed, skipped := 0, 0
	for {
		pid := entry.ProcessID
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))

		if pid > 4 && pid != selfPID && pid != fgPID && !protectedProcNames[name] {
			h, openErr := windows.OpenProcess(
				windows.PROCESS_SET_QUOTA|windows.PROCESS_QUERY_LIMITED_INFORMATION,
				false, pid,
			)
			if openErr != nil {
				skipped++
			} else {
				r1, _, _ := procEmptyWorkingSet.Call(uintptr(h))
				if r1 != 0 {
					trimmed++
				} else {
					skipped++
				}
				windows.CloseHandle(h)
			}
		} else {
			skipped++
		}

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	return trimmed, skipped
}

func GetMemoryStats() MemoryStats {
	var memStatus MEMORYSTATUSEX
	memStatus.Length = uint32(unsafe.Sizeof(memStatus))

	procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))

	totalMB := memStatus.TotalPhys / (1024 * 1024)
	freeMB := memStatus.AvailPhys / (1024 * 1024)
	usedMB := totalMB - freeMB
	usedPct := math.Round((float64(usedMB)/float64(totalMB))*1000) / 10

	return MemoryStats{
		TotalMB:     totalMB,
		FreeMB:      freeMB,
		UsedMB:      usedMB,
		UsedPercent: usedPct,
	}
}

func FormatNTSTATUS(code int32) string {
	if code == 0 {
		return "SUCCESS (0x00000000)"
	}
	return fmt.Sprintf("0x%08X", uint32(code))
}

func GetSystemCpuUsage() uint64 {
	var idle1, kernel1, user1 syscall.Filetime
	var idle2, kernel2, user2 syscall.Filetime

	r1, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle1)),
		uintptr(unsafe.Pointer(&kernel1)),
		uintptr(unsafe.Pointer(&user1)),
	)
	if r1 == 0 {
		return 0
	}

	time.Sleep(100 * time.Millisecond)

	r2, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle2)),
		uintptr(unsafe.Pointer(&kernel2)),
		uintptr(unsafe.Pointer(&user2)),
	)
	if r2 == 0 {
		return 0
	}

	i1 := uint64(idle1.HighDateTime)<<32 | uint64(idle1.LowDateTime)
	i2 := uint64(idle2.HighDateTime)<<32 | uint64(idle2.LowDateTime)
	k1 := uint64(kernel1.HighDateTime)<<32 | uint64(kernel1.LowDateTime)
	k2 := uint64(kernel2.HighDateTime)<<32 | uint64(kernel2.LowDateTime)
	u1 := uint64(user1.HighDateTime)<<32 | uint64(user1.LowDateTime)
	u2 := uint64(user2.HighDateTime)<<32 | uint64(user2.LowDateTime)

	idle := i2 - i1
	kernel := k2 - k1
	user := u2 - u1

	total := kernel + user
	if total == 0 {
		return 0
	}

	pct := float64(total-idle) / float64(total) * 100.0
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return uint64(math.Round(pct))
}

func GetSystemStats() SystemStats {
	cpu := GetSystemCpuUsage()
	mem := GetMemoryStats()

	totalGB := math.Round((float64(mem.TotalMB)/1024.0)*100) / 100
	usedGB := math.Round((float64(mem.UsedMB)/1024.0)*100) / 100

	var pct uint64
	if mem.TotalMB > 0 {
		pct = uint64(math.Round((float64(mem.UsedMB) / float64(mem.TotalMB)) * 100))
	}

	return SystemStats{
		CpuLoad: cpu,
		UsedGB:  usedGB,
		TotalGB: totalGB,
		Pct:     pct,
	}
}

// RunRamCleaner runs whichever of the four memory operations are enabled in
// cfg, in the order that makes each one count: working sets are trimmed and
// dirty pages flushed FIRST so those pages land on the standby list, and the
// standby purge runs LAST so it sweeps everything the earlier steps produced.
func RunRamCleaner(cfg *Config) RamResult {
	before := GetMemoryStats()
	result := RamResult{BeforePct: before.UsedPercent, AfterPct: before.UsedPercent}

	// Build the list of enabled operations up front so the step counter
	// ("Step 2/4") reflects what's actually going to run.
	type ramStep struct {
		title string
		note  string
		run   func()
	}
	var steps []ramStep

	if cfg.RamTrimWorkingSets == 1 {
		result.OpsRun = append(result.OpsRun, T(locales.RamOpTrim))
		steps = append(steps, ramStep{
			T(locales.RamTrimTitle),
			T(locales.RamTrimNote),
			func() {
				trimmed, skipped := TrimAllWorkingSets()
				result.Trimmed = trimmed
				result.TrimSkipped = skipped
				if trimmed > 0 {
					fmt.Printf("    \x1b[31m%s\x1b[0m\n", TD(locales.RamTrimDone, map[string]any{"Trimmed": trimmed, "Skipped": skipped}))
				} else {
					fmt.Printf("    \x1b[33m%s\x1b[0m\n", TD(locales.RamTrimNone, map[string]any{"Skipped": skipped}))
				}
			},
		})
	}

	if cfg.RamFlushModified == 1 {
		result.OpsRun = append(result.OpsRun, T(locales.RamOpFlush))
		steps = append(steps, ramStep{
			T(locales.RamFlushTitle),
			T(locales.RamFlushNote),
			func() {
				res := FlushModifiedList()
				status := FormatNTSTATUS(res)
				if res == 0 {
					fmt.Printf("    \x1b[31m%s\x1b[0m\n", TD(locales.RamFlushDone, map[string]any{"Status": status}))
				} else {
					fmt.Printf("    \x1b[33m%s\x1b[0m\n", TD(locales.RamFlushFail, map[string]any{"Status": status}))
				}
			},
		})
	}

	if cfg.RamFileCache == 1 {
		result.OpsRun = append(result.OpsRun, T(locales.RamOpFileCache))
		steps = append(steps, ramStep{
			T(locales.RamCacheTitle),
			T(locales.RamCacheNote),
			func() {
				if PurgeSystemFileCache() {
					fmt.Printf("    \x1b[31m%s\x1b[0m\n", T(locales.RamCacheDone))
				} else {
					fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamCacheFail))
				}
			},
		})
	}

	if cfg.RamPurgeStandby == 1 {
		result.OpsRun = append(result.OpsRun, T(locales.RamOpStandby))
		steps = append(steps, ramStep{
			T(locales.RamStandbyTitle),
			T(locales.RamStandbyNote),
			func() {
				res := PurgeStandbyList()
				status := FormatNTSTATUS(res)
				if res == 0 {
					fmt.Printf("    \x1b[31m%s\x1b[0m\n", TD(locales.RamStandbyDone, map[string]any{"Status": status}))
				} else {
					fmt.Printf("    \x1b[33m%s\x1b[0m\n", TD(locales.RamStandbyFail, map[string]any{"Status": status}))
				}
			},
		})
	}

	if len(steps) == 0 {
		fmt.Println()
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamAllOff))
		result.NothingToRun = true
		return result
	}

	total := len(steps) + 1

	// --- Step 1: Enable privilege ---
	fmt.Println()
	fmt.Printf("  \x1b[36m%s\x1b[0m\n", TD(locales.RamPrivStep, map[string]any{"Step": 1, "Total": total}))
	if EnablePrivilege() {
		fmt.Printf("    \x1b[31m%s\x1b[0m\n", T(locales.RamPrivOk))
	} else {
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamPrivFail))
		result.PrivFailed = true
		return result
	}

	for i, s := range steps {
		fmt.Println()
		fmt.Printf("  \x1b[36m%s\x1b[0m\n", TD(locales.RamStepTitle, map[string]any{"Step": i + 2, "Total": total, "Title": s.title}))
		fmt.Printf("    %s\n", s.note)
		s.run()
	}

	time.Sleep(800 * time.Millisecond)

	after := GetMemoryStats()

	var freedMB int64
	if after.FreeMB >= before.FreeMB {
		freedMB = int64(after.FreeMB - before.FreeMB)
	} else {
		freedMB = -int64(before.FreeMB - after.FreeMB)
	}

	dropPct := math.Round((before.UsedPercent-after.UsedPercent)*10) / 10

	result.FreedMB = freedMB
	result.AfterPct = after.UsedPercent
	result.DropPct = dropPct

	fmt.Println()
	fmt.Println("  +---------------------------------------+")
	fmt.Printf("             \x1b[36m%s\x1b[0m\n", T(locales.RamResults))
	fmt.Println("  +---------------------------------------+")

	if freedMB > 0 {
		fmt.Printf("    \x1b[31m%s\x1b[0m\n", TD(locales.RamFreed, map[string]any{"MB": freedMB}))
		fmt.Printf("    \x1b[31m%s\x1b[0m\n", TD(locales.RamDrop, map[string]any{
			"Drop":   fmt.Sprintf("%.1f", dropPct),
			"Before": fmt.Sprintf("%.1f", before.UsedPercent),
			"After":  fmt.Sprintf("%.1f", after.UsedPercent),
		}))
		fmt.Printf("    \x1b[31m%s\x1b[0m\n", T(locales.RamSuccess))
	} else if freedMB == 0 {
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamZero))
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamZeroStatus))
	} else {
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", TD(locales.RamChange, map[string]any{"MB": freedMB}))
		fmt.Printf("    \x1b[33m%s\x1b[0m\n", T(locales.RamChangeStatus))
	}

	// No CPU/Memory box here on purpose: the same numbers are already in the
	// SYSTEM RESULTS block above and in the end-of-run summary, and the live
	// row on the main menu is the one place stats belong.

	return result
}
