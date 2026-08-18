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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type StepResults struct {
	TotalDeleted      int64
	TotalFailed       int64
	DnsFailed         bool
	RecycleBinFailed  bool
	RecycleBinBytes   uint64
	RamFreedMB        int64
	Ram               RamResult
	DeepCleanupRan    bool
	DeepCleanupFailed bool
	StepCounts        [12]int64
}

func stopService(name string) {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		return
	}

	// Wait for service to actually stop (up to 5 seconds)
	for i := 0; i < 50; i++ {
		status, err := s.Query()
		if err != nil {
			return
		}
		if status.State == svc.Stopped {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startService(name string) {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return
	}
	defer s.Close()

	_ = s.Start()
}

func getEnvOrDefault(envKey, defaultValue string) string {
	val := os.Getenv(envKey)
	if val == "" {
		return defaultValue
	}
	return val
}

// setupSagerun99 configures the Windows registry so that cleanmgr /sagerun:99
// will completely and silently clean all available categories.
func setupSagerun99() error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\VolumeCaches`, registry.READ|registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}

	for _, name := range subkeys {
		sk, err := registry.OpenKey(k, name, registry.SET_VALUE)
		if err == nil {
			_ = sk.SetDWordValue("StateFlags0099", 2)
			sk.Close()
		}
	}
	return nil
}

func cleanDirContents(dirPath string, printFileOK bool, results *StepResults, stepIdx int) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	// Concurrency worker semaphore to avoid file handle exhaustion
	sem := make(chan struct{}, 32)

	for _, entry := range entries {
		itemPath := filepath.Join(dirPath, entry.Name())
		entryName := entry.Name()

		if !entry.IsDir() {
			wg.Add(1)
			sem <- struct{}{}
			go func(p, name string) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						atomic.AddInt64(&results.TotalFailed, 1)
					}
				}()

				err := os.Remove(p)
				if err == nil {
					atomic.AddInt64(&results.TotalDeleted, 1)
					atomic.AddInt64(&results.StepCounts[stepIdx], 1)
					if printFileOK {
						fmt.Printf("\x1b[31m[OK]\x1b[0m %s\n", name)
					}
				} else {
					atomic.AddInt64(&results.TotalFailed, 1)
				}
			}(itemPath, entryName)
		} else {
			// Subdirectory deletion
			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						atomic.AddInt64(&results.TotalFailed, 1)
					}
				}()

				err := os.RemoveAll(p)
				if err == nil {
					atomic.AddInt64(&results.TotalDeleted, 1)
					atomic.AddInt64(&results.StepCounts[stepIdx], 1)
				} else {
					atomic.AddInt64(&results.TotalFailed, 1)
				}
			}(itemPath)
		}
	}

	wg.Wait()
}

func cleanDirRecursiveFiles(dirPath string, printFileOK bool, results *StepResults, stepIdx int) {
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return
	}

	var filePaths []string
	_ = filepath.Walk(dirPath, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			filePaths = append(filePaths, p)
		}
		return nil
	})

	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)

	for _, p := range filePaths {
		wg.Add(1)
		sem <- struct{}{}
		name := filepath.Base(p)
		go func(path, fileName string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&results.TotalFailed, 1)
				}
			}()

			err := os.Remove(path)
			if err == nil {
				atomic.AddInt64(&results.TotalDeleted, 1)
				atomic.AddInt64(&results.StepCounts[stepIdx], 1)
				if printFileOK {
					fmt.Printf("\x1b[31m[OK]\x1b[0m %s\n", fileName)
				}
			} else {
				atomic.AddInt64(&results.TotalFailed, 1)
			}
		}(p, name)
	}

	wg.Wait()
}

func RunCleaningPipeline(cfg *Config) (results StepResults) {
	// Pipeline-level crash guard: if any step panics, return partial results
	// so the summary can still be displayed.
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n\x1b[31m[ERROR]\x1b[0m %s\n", Tf("cleaner.crashed", r))
			fmt.Println(T("cleaner.partial"))
		}
	}()

	systemRoot := getEnvOrDefault("SystemRoot", `C:\Windows`)
	userTemp := os.TempDir()
	programData := getEnvOrDefault("ProgramData", `C:\ProgramData`)

	// No artificial delay — all steps run raw and instant.

	// [1/12] Windows Temp folder
	fmt.Println()
	if cfg.WinTemp == 1 {
		StepHeader(1)
		fmt.Println("+---------------------------------------+")
		winTempDir := filepath.Join(systemRoot, "Temp")
		cleanDirContents(winTempDir, true, &results, 1)
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(1)
	}

	// [2/12] User Temp folder
	fmt.Println()
	if cfg.UserTemp == 1 {
		StepHeader(2)
		fmt.Println("+---------------------------------------+")
		cleanDirContents(userTemp, true, &results, 2)
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(2)
	}

	// [3/12] Prefetch folder
	fmt.Println()
	if cfg.Prefetch == 1 {
		StepHeader(3)
		fmt.Println("+---------------------------------------+")
		prefetchDir := filepath.Join(systemRoot, "Prefetch")
		cleanDirContents(prefetchDir, true, &results, 3)
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(3)
	}

	// [4/12] Windows Error Reports
	fmt.Println()
	if cfg.ErrorReports == 1 {
		StepHeader(4)
		fmt.Println("+---------------------------------------+")
		werDir := filepath.Join(programData, "Microsoft", "Windows", "WER")
		cleanDirRecursiveFiles(werDir, true, &results, 4)
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(4)
	}

	// [5/12] Delivery Optimization Cache
	fmt.Println()
	if cfg.DeliveryOpt == 1 {
		StepHeader(5)
		fmt.Println("+---------------------------------------+")
		stopService("DoSvc")
		doDir := filepath.Join(systemRoot, "SoftwareDistribution", "DeliveryOptimization")
		cleanDirRecursiveFiles(doDir, true, &results, 5)
		startService("DoSvc")
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(5)
	}

	// [6/12] Windows Update Cache
	fmt.Println()
	if cfg.WinUpdate == 1 {
		StepHeader(6)
		fmt.Println("+---------------------------------------+")
		stopService("wuauserv")
		wuDir := filepath.Join(systemRoot, "SoftwareDistribution", "Download")
		cleanDirRecursiveFiles(wuDir, true, &results, 6)
		startService("wuauserv")
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(6)
	}

	// [7/12] Windows Log Files
	fmt.Println()
	if cfg.LogFiles == 1 {
		StepHeader(7)
		fmt.Println("+---------------------------------------+")
		logsDir := filepath.Join(systemRoot, "Logs")
		cleanDirRecursiveFiles(logsDir, true, &results, 7)
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(7)
	}

	// [8/12] Windows Installer Temp
	fmt.Println()
	if cfg.InstallerTemp == 1 {
		StepHeader(8)
		fmt.Println("+---------------------------------------+")
		stopService("msiserver")
		instDir := filepath.Join(systemRoot, "Installer", "$PatchCache$")
		cleanDirRecursiveFiles(instDir, true, &results, 8)
		startService("msiserver")
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(8)
	}

	// [9/12] DNS Cache
	fmt.Println()
	if cfg.DnsFlush == 1 {
		StepHeader(9)
		fmt.Println("+---------------------------------------+")
		ok := FlushDNS()
		if ok {
			fmt.Printf("\x1b[31m[OK]\x1b[0m %s\n", T("cleaner.dns_ok"))
		} else {
			fmt.Println(T("cleaner.dns_fail"))
			results.DnsFailed = true
		}
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(9)
	}

	// [10/12] RAM Optimization
	fmt.Println()
	if cfg.RamOptimize == 1 {
		StepHeader(10)
		fmt.Println("+---------------------------------------+")
		results.Ram = RunRamCleaner(cfg)
		results.RamFreedMB = results.Ram.FreedMB
	} else {
		StepSkipped(10)
	}

	// [11/11] Recycle Bin (OFF by default - destructive/irreversible)
	fmt.Println()
	if cfg.RecycleBin == 1 {
		StepHeader(11)
		fmt.Println("+---------------------------------------+")
		bytesFreed, ok := EmptyRecycleBin()
		if ok {
			results.RecycleBinBytes = bytesFreed
			if bytesFreed > 0 {
				fmt.Printf("\x1b[31m[OK]\x1b[0m %s\n", Tf("cleaner.bin_freed", FormatBytesHuman(bytesFreed)))
			} else {
				fmt.Printf("\x1b[31m[OK]\x1b[0m %s\n", T("cleaner.bin_empty"))
			}
		} else {
			fmt.Println(T("cleaner.bin_fail"))
			results.RecycleBinFailed = true
		}
		fmt.Println(T("common.done"))
	} else {
		StepSkipped(11)
	}

	// [12/12] Deep Cleanup - AGGRESSIVE — one-time opt-in, always resets
	fmt.Println()
	if cfg.DeepCleanup == 1 {
		TypeLine("\x1b[31m[12/12] Deep Cleanup - AGGRESSIVE...\x1b[0m", 0)
		fmt.Println("+---------------------------------------+")

		// Configure registry for completely silent, maximum cleaning
		if err := setupSagerun99(); err != nil {
			fmt.Printf("\x1b[36m[WARN]\x1b[0m %s\n", Tf("cleaner.reg_fail", err))
		}

		// --- Command 1: cleanmgr.exe /sagerun:99 ---
		// cleanmgr.exe is a GUI stub: it spawns a background worker
		// (dismhost.exe / cleanmgr.exe worker) and the parent exits
		// immediately with success, so cmd.Run() alone proves nothing.
		// We launch it, then poll for the worker process to disappear.
		fmt.Printf("  %s\n", T("cleaner.cleanmgr_run"))
		cmd1 := exec.Command("cleanmgr.exe", "/sagerun:99")
		cmd1.Stdout = os.Stdout
		cmd1.Stderr = os.Stderr
		if err := cmd1.Run(); err != nil {
			fmt.Printf("  \x1b[36m[WARN]\x1b[0m %s\n", Tf("cleaner.cleanmgr_fail", err))
			results.DeepCleanupFailed = true
		} else {
			// Wait for background cleanmgr worker to finish.
			// Poll every 2 seconds for up to 5 minutes.
			fmt.Printf("  %s\n", T("cleaner.cleanmgr_wait"))
			cleanmgrDone := false
			for i := 0; i < 150; i++ {
				time.Sleep(2 * time.Second)
				// tasklist filter: check if any cleanmgr.exe process is running
				check := exec.Command("tasklist", "/FI", "IMAGENAME eq cleanmgr.exe", "/NH")
				out, _ := check.Output()
				if len(out) == 0 || !containsIgnoreCase(string(out), "cleanmgr.exe") {
					cleanmgrDone = true
					break
				}
			}
			if cleanmgrDone {
				fmt.Printf("  \x1b[31m[OK]\x1b[0m %s\n", T("cleaner.cleanmgr_ok"))
			} else {
				fmt.Printf("  \x1b[36m[WARN]\x1b[0m %s\n", T("cleaner.cleanmgr_time"))
				results.DeepCleanupFailed = true
			}
		}

		results.DeepCleanupRan = true
		fmt.Println(T("common.done"))

		// CRITICAL: always reset back to OFF after running
		cfg.DeepCleanup = 0
	} else {
		StepSkipped(12)
	}

	return results
}

// containsIgnoreCase reports whether s contains substr, ignoring case.
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
