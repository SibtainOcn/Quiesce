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

// The task runner is the reusable core of every "do a list of jobs" screen.
// A feature describes its work as []Task and calls RunTasks; the runner
// handles the live screen, skipping, pause/abort, timing, the optional log
// file and crash isolation. Cleaning is one such list (cleaner.go); browser
// or app cache cleaning and the telemetry scan can be others.

// Task is one job in a run.
type Task struct {
	Name    string // short label: checklist and status line
	Title   string // banner printed in the log when the task starts
	Alert   bool   // print the banner in red (destructive tasks)
	Enabled bool   // false: shown as skipped, never run
	Run     func(tc *TaskCtx) TaskOutcome
}

// TaskOutcome is what a finished task reports back to the screen.
type TaskOutcome struct {
	Detail string // short value shown beside the task, e.g. "142" or "1.2 GB"
	Warn   bool   // finished with a problem: shown in yellow
}

// RunReport summarises a whole run.
type RunReport struct {
	Ran     []bool // per task: whether it started
	Aborted bool
	Bytes   int64
	LogPath string // empty when no log file was kept
	LogErr  error
}

// RunOptions configures a run.
type RunOptions struct {
	Style  OutputStyle // verbose file log, or minimal checklist
	Title  string      // heads the minimal checklist, e.g. "Cleaning"
	LogDir string      // when not empty, keep a plain-text log there
}

// TaskCtx is what a running task uses to report progress and to cooperate
// with pause/abort. File helpers hang off it (see fileops.go) so every
// feature deletes files the same way.
type TaskCtx struct {
	v *LiveView
}

func (tc *TaskCtx) Log(kind LogKind, text string) { tc.v.Log(kind, text) }

// Checkpoint blocks while paused and returns false once aborted. Long tasks
// should call it between units of work.
func (tc *TaskCtx) Checkpoint() bool { return tc.v.Checkpoint() }
func (tc *TaskCtx) Aborted() bool    { return tc.v.Aborted() }

// SetTotal sets how many items this task will process, for the progress bar.
func (tc *TaskCtx) SetTotal(n int) { tc.v.SetTotal(n) }

// WithService stops a Windows service, runs fn, and restarts the service -
// even if fn panics, the run is aborted, or the window is closed (the
// shutdown handler restarts every service still marked stopped).
func (tc *TaskCtx) WithService(name string, fn func()) {
	markServiceStopped(name)
	defer func() {
		startService(name)
		markServiceRestarted(name)
	}()
	stopService(name)
	fn()
}

// RunTasks runs tasks in order with live progress. The console is handed
// back in its normal state afterwards, ready for the caller's summary.
func RunTasks(tasks []Task, opts RunOptions) (rep RunReport) {
	names := make([]string, len(tasks))
	enabled := make([]bool, len(tasks))
	for i, t := range tasks {
		names[i] = t.Name
		enabled[i] = t.Enabled
	}

	v := NewLiveView(opts.Style, opts.Title, names, enabled, opts.LogDir)
	rep.Ran = make([]bool, len(tasks))

	// Always restore the console and flush the log, even if something below
	// panics outside a task.
	defer func() {
		v.Finish()
		rep.Aborted = v.Aborted()
		_, rep.Bytes, _ = v.Totals()
		rep.LogPath, rep.LogErr = v.Close()
	}()

	tc := &TaskCtx{v: v}
	for i, t := range tasks {
		if !t.Enabled {
			v.SkipTask(i)
			continue
		}
		if !v.Checkpoint() {
			v.NotRun(i)
			continue
		}
		v.BeginTask(i, t.Title, t.Alert)
		rep.Ran[i] = true
		out := runTaskSafely(t, tc)
		if v.Aborted() {
			out = TaskOutcome{Detail: T("summary.partial"), Warn: true}
		}
		v.EndTask(i, out.Detail, out.Warn)
	}
	return rep
}

// runTaskSafely isolates a crash to its own task: it is logged and marked
// failed, and the run moves on to the next task.
func runTaskSafely(t Task, tc *TaskCtx) (out TaskOutcome) {
	defer func() {
		if r := recover(); r != nil {
			tc.Log(LogError, Tf("cleaner.crashed", r))
			out = TaskOutcome{Detail: T("summary.failed"), Warn: true}
		}
	}()
	if t.Run == nil {
		return TaskOutcome{}
	}
	return t.Run(tc)
}
