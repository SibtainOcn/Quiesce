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
	StepCounts        [13]int64 // indexed by step number, 1-12

	// NotRun marks enabled steps that never started because the run was
	// aborted. Zero value means "ran", so older callers need no changes.
	NotRun  [13]bool
	Aborted bool
	LogPath string // log file written for this run, if any
	LogErr  string // why the log file could not be written, if it failed

	// ExitConfirmed: the user already pressed ENTER on the live summary.
	ExitConfirmed bool
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

// RunCleaningPipeline runs the cleaning steps on the live screen and returns
// the per-step results for the summary. Each step is an ordinary Task, so
// the screen, pause/abort and crash handling all come from RunTasks.
func RunCleaningPipeline(cfg *Config) (results StepResults) {
	systemRoot := getEnvOrDefault("SystemRoot", `C:\Windows`)
	userTemp := os.TempDir()
	programData := getEnvOrDefault("ProgramData", `C:\ProgramData`)

	// step wraps a step's work as a Task with its translated labels.
	step := func(idx int, on bool, run func(tc *TaskCtx) TaskOutcome) Task {
		return Task{
			Name:    T(fmt.Sprintf("step.%d.short", idx)),
			Title:   stepHeaderText(idx),
			Alert:   idx == 12,
			Enabled: on,
			Run:     run,
		}
	}

	// files turns a folder-cleaning function into a step body that records
	// its counts for the summary.
	files := func(idx int, clean func(tc *TaskCtx) (deleted, failed int64)) func(*TaskCtx) TaskOutcome {
		return func(tc *TaskCtx) TaskOutcome {
			d, f := clean(tc)
			results.TotalDeleted += d
			results.TotalFailed += f
			results.StepCounts[idx] += d
			return TaskOutcome{Detail: fmt.Sprintf("%d", d)}
		}
	}

	// serviceTree cleans a folder tree while its owning service is stopped.
	serviceTree := func(service, dir string) func(tc *TaskCtx) (int64, int64) {
		return func(tc *TaskCtx) (d, f int64) {
			tc.WithService(service, func() { d, f = tc.CleanTree(dir) })
			return d, f
		}
	}

	tasks := []Task{
		step(1, cfg.WinTemp == 1, files(1, func(tc *TaskCtx) (int64, int64) {
			return tc.CleanDir(filepath.Join(systemRoot, "Temp"))
		})),
		step(2, cfg.UserTemp == 1, files(2, func(tc *TaskCtx) (int64, int64) {
			return tc.CleanDir(userTemp)
		})),
		step(3, cfg.Prefetch == 1, files(3, func(tc *TaskCtx) (int64, int64) {
			return tc.CleanDir(filepath.Join(systemRoot, "Prefetch"))
		})),
		step(4, cfg.ErrorReports == 1, files(4, func(tc *TaskCtx) (int64, int64) {
			return tc.CleanTree(filepath.Join(programData, "Microsoft", "Windows", "WER"))
		})),
		step(5, cfg.DeliveryOpt == 1, files(5,
			serviceTree("DoSvc", filepath.Join(systemRoot, "SoftwareDistribution", "DeliveryOptimization")))),
		step(6, cfg.WinUpdate == 1, files(6,
			serviceTree("wuauserv", filepath.Join(systemRoot, "SoftwareDistribution", "Download")))),
		step(7, cfg.LogFiles == 1, files(7, func(tc *TaskCtx) (int64, int64) {
			return tc.CleanTree(filepath.Join(systemRoot, "Logs"))
		})),
		step(8, cfg.InstallerTemp == 1, files(8,
			serviceTree("msiserver", filepath.Join(systemRoot, "Installer", "$PatchCache$")))),

		step(9, cfg.DnsFlush == 1, func(tc *TaskCtx) TaskOutcome {
			if FlushDNS() {
				tc.Log(LogOK, T("cleaner.dns_ok"))
				return TaskOutcome{Detail: T("summary.flushed")}
			}
			tc.Log(LogPlain, T("cleaner.dns_fail"))
			results.DnsFailed = true
			return TaskOutcome{Detail: T("summary.failed"), Warn: true}
		}),

		step(10, cfg.RamOptimize == 1, func(tc *TaskCtx) TaskOutcome {
			results.Ram = RunRamCleaner(cfg, tc)
			results.RamFreedMB = results.Ram.FreedMB
			switch r := results.Ram; {
			case r.NothingToRun:
				return TaskOutcome{Detail: T("common.off")}
			case r.PrivFailed:
				return TaskOutcome{Detail: T("summary.failed"), Warn: true}
			case r.FreedMB > 0:
				return TaskOutcome{Detail: fmt.Sprintf("%d MB", r.FreedMB)}
			default:
				return TaskOutcome{Detail: "0 MB"}
			}
		}),

		// Recycle Bin is OFF by default: destructive and irreversible.
		step(11, cfg.RecycleBin == 1, func(tc *TaskCtx) TaskOutcome {
			bytesFreed, ok := EmptyRecycleBin()
			if !ok {
				tc.Log(LogPlain, T("cleaner.bin_fail"))
				results.RecycleBinFailed = true
				return TaskOutcome{Detail: T("summary.failed"), Warn: true}
			}
			results.RecycleBinBytes = bytesFreed
			tc.v.AddBytes(int64(bytesFreed))
			if bytesFreed > 0 {
				tc.Log(LogOK, Tf("cleaner.bin_freed", FormatBytesHuman(bytesFreed)))
			} else {
				tc.Log(LogOK, T("cleaner.bin_empty"))
			}
			return TaskOutcome{Detail: FormatBytesHuman(bytesFreed)}
		}),

		step(12, cfg.DeepCleanup == 1, func(tc *TaskCtx) TaskOutcome {
			return runDeepCleanup(tc, &results)
		}),
	}

	// collect copies the run report into the results the summary reads.
	collect := func(rep RunReport) {
		for i, t := range tasks {
			if t.Enabled && i < len(rep.Ran) && !rep.Ran[i] {
				results.NotRun[i+1] = true
			}
		}
		results.Aborted = rep.Aborted
		results.LogPath = rep.LogPath
		results.LogErr = ""
		if rep.LogErr != nil {
			results.LogErr = rep.LogErr.Error()
		}
	}

	opts := RunOptions{
		Summary: func(rep RunReport) []string {
			collect(rep)
			return SummaryLines(cfg, results)
		},
	}
	if cfg.KeepLog == 1 {
		opts.LogDir = LogDirPath()
	}
	rep := RunTasks(tasks, opts)
	collect(rep)
	results.ExitConfirmed = rep.ExitConfirmed

	// Deep Cleanup is one-shot: always back to OFF after a run, including a
	// run that was aborted before reaching it.
	cfg.DeepCleanup = 0
	return results
}

// cleanmgr wait limits. cleanmgr.exe is a GUI stub: it hands the work to a
// background worker and exits at once, so its exit proves nothing - the
// worker process is polled until it disappears.
const (
	cleanmgrTimeout = 5 * time.Minute
	cleanmgrPoll    = 2 * time.Second
)

// runDeepCleanup runs Windows Disk Cleanup with every handler enabled.
func runDeepCleanup(tc *TaskCtx, results *StepResults) TaskOutcome {
	results.DeepCleanupRan = true
	partial := func() TaskOutcome {
		results.DeepCleanupFailed = true
		return TaskOutcome{Detail: T("summary.partial"), Warn: true}
	}

	// Configure registry for completely silent, maximum cleaning.
	if err := setupSagerun99(); err != nil {
		tc.Log(LogWarn, Tf("cleaner.reg_fail", err))
	}

	tc.Log(LogPlain, "  "+T("cleaner.cleanmgr_run"))
	// No Stdout/Stderr: anything it printed would tear the live screen.
	if err := exec.Command("cleanmgr.exe", "/sagerun:99").Run(); err != nil {
		tc.Log(LogWarn, Tf("cleaner.cleanmgr_fail", err))
		return partial()
	}

	tc.Log(LogPlain, "  "+T("cleaner.cleanmgr_wait"))
	deadline := time.Now().Add(cleanmgrTimeout)
	for cleanmgrRunning() {
		if time.Now().After(deadline) {
			tc.Log(LogWarn, T("cleaner.cleanmgr_time"))
			return partial()
		}
		// Sleep in short slices so Q is answered promptly.
		for slept := time.Duration(0); slept < cleanmgrPoll; slept += 200 * time.Millisecond {
			if !tc.Checkpoint() {
				tc.Log(LogWarn, T("cleaner.cleanmgr_left"))
				return partial()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	tc.Log(LogOK, T("cleaner.cleanmgr_ok"))
	return TaskOutcome{Detail: T("summary.completed")}
}

// cleanmgrRunning reports whether a cleanmgr.exe process exists. If
// tasklist itself fails, it is treated as finished rather than waiting out
// the full timeout.
func cleanmgrRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq cleanmgr.exe", "/NH").Output()
	if err != nil {
		return false
	}
	return containsIgnoreCase(string(out), "cleanmgr.exe")
}

// LogDirPath is where run logs are kept: a "logs" folder next to qc.exe.
func LogDirPath() string {
	return filepath.Join(filepath.Dir(ConfigFilePath()), "logs")
}

// containsIgnoreCase reports whether s contains substr, ignoring case.
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
