package main

import (
	"fmt"
	"math"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modntdll                   = windows.NewLazySystemDLL("ntdll.dll")
	modkernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procNtSetSystemInformation = modntdll.NewProc("NtSetSystemInformation")
	procGlobalMemoryStatusEx   = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes         = modkernel32.NewProc("GetSystemTimes")
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

type SystemStats struct {
	CpuLoad uint64
	UsedGB  float64
	TotalGB float64
	Pct     uint64
}

func EnablePrivilege() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	privName, _ := windows.UTF16PtrFromString("SeProfileSingleProcessPrivilege")
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

func RunRamCleaner() int64 {
	before := GetMemoryStats()

	// --- Step 1: Enable privilege ---
	fmt.Println()
	fmt.Println("  \x1b[36m[Step 1/3] Enabling SeProfileSingleProcessPrivilege...\x1b[0m")
	privOk := EnablePrivilege()
	if privOk {
		fmt.Println("    \x1b[31mPrivilege enabled successfully\x1b[0m")
	} else {
		fmt.Println("    \x1b[33mFAILED to enable privilege\x1b[0m")
		return 0
	}

	// --- Step 2: Flush modified page list ---
	fmt.Println()
	fmt.Println("  \x1b[36m[Step 2/3] Flushing modified page list...\x1b[0m")
	fmt.Println("    (Writing dirty pages to disk so they can be freed)")
	res1 := FlushModifiedList()
	status1 := FormatNTSTATUS(res1)
	if res1 == 0 {
		fmt.Printf("    \x1b[31mModified page list flushed - NTSTATUS: %s\x1b[0m\n", status1)
	} else {
		fmt.Printf("    \x1b[33mFlushModifiedList returned NTSTATUS: %s\x1b[0m\n", status1)
	}

	// --- Step 3: Purge standby list ---
	fmt.Println()
	fmt.Println("  \x1b[36m[Step 3/3] Purging standby list...\x1b[0m")
	fmt.Println("    (Freeing cached/standby memory - this is the main operation)")
	res2 := PurgeStandbyList()
	status2 := FormatNTSTATUS(res2)
	if res2 == 0 {
		fmt.Printf("    \x1b[31mStandby list purged - NTSTATUS: %s\x1b[0m\n", status2)
	} else {
		fmt.Printf("    \x1b[33mPurgeStandbyList returned NTSTATUS: %s\x1b[0m\n", status2)
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

	fmt.Println()
	fmt.Println("  +---------------------------------------+")
	fmt.Println("             \x1b[36mSYSTEM RESULTS\x1b[0m")
	fmt.Println("  +---------------------------------------+")

	if freedMB > 0 {
		fmt.Printf("    \x1b[31mRAM Freed    : %d MB\x1b[0m\n", freedMB)
		fmt.Printf("    \x1b[31mUsage Drop   : %.1f%% (%.1f%% -> %.1f%%)\x1b[0m\n", dropPct, before.UsedPercent, after.UsedPercent)
		fmt.Println("    \x1b[31mStatus       : SUCCESS - RAM was actually freed\x1b[0m")
	} else if freedMB == 0 {
		fmt.Println("    \x1b[33mRAM Freed    : 0 MB (standby list was already empty)\x1b[0m")
		fmt.Println("    \x1b[33mStatus       : Nothing to free - this is normal if run recently\x1b[0m")
	} else {
		fmt.Printf("    \x1b[33mRAM Change   : %d MB (background app allocated during cleanup)\x1b[0m\n", freedMB)
		fmt.Println("    \x1b[33mStatus       : Try closing background apps and run again\x1b[0m")
	}

	stats := GetSystemStats()
	fmt.Println()
	fmt.Println("  +---------------------------------------+")
	fmt.Printf("  \x1b[31mCPU : %d%%  -  Memory : %.2f/%.2f GB (%d%%)\x1b[0m\n", stats.CpuLoad, stats.UsedGB, stats.TotalGB, stats.Pct)
	fmt.Println("  +---------------------------------------+")

	return freedMB
}
