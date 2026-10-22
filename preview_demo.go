//go:build demo

package main

// Fake-run preview of the progress display. Deletes nothing. Built only with
// -tags demo:  go build -tags demo -o qc_preview.exe .
//   qc_preview.exe          verbose style
//   qc_preview.exe min      minimal style

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

func init() {
	EnableVirtualTerminalProcessing()
	EnableUTF8Console()
	DisableQuickEditMode()
	InstallShutdownHandler()

	style := StyleVerbose
	if len(os.Args) > 1 && os.Args[1] == "min" {
		style = StyleMinimal
	}
	counts := []int{60, 200, 90, 10, 40, 50, 30, 20, 0, 0, 0, 0}
	var tasks []Task
	for i, n := range counts {
		tasks = append(tasks, Task{
			Name:    T(fmt.Sprintf("step.%d.short", i+1)),
			Title:   stepHeaderText(i + 1),
			Enabled: i < 10,
			Run: func(tc *TaskCtx) TaskOutcome {
				if n == 0 {
					time.Sleep(800 * time.Millisecond)
					return TaskOutcome{Detail: T("summary.flushed")}
				}
				tc.SetTotal(n)
				d := 0
				for ; d < n && tc.Checkpoint(); d++ {
					tc.v.Deleted(fmt.Sprintf("setup_%06X.tmp", rand.Intn(0xffffff)), int64(rand.Intn(900000)))
					time.Sleep(40 * time.Millisecond)
				}
				return TaskOutcome{Detail: Tf("live.files", d)}
			},
		})
	}
	RunTasks(tasks, RunOptions{Style: style, Title: T("live.title_clean")})
	fmt.Print("\n" + T("common.press_enter"))
	WaitForEnter()
	RestoreConsoleMode()
	os.Exit(0)
}
