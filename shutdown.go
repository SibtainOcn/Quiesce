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

// Clean exit when the user closes the window, presses Ctrl+C, logs off or
// shuts down mid-run. Without this, a run interrupted while Windows Update
// (or another service) was stopped for cleaning would leave it stopped.
//
// Go reports a closed window, logoff and shutdown as SIGTERM, and keeps the
// process alive while the handler runs - Windows allows about 5 seconds -
// so there is time to restart services and save the log.

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	svcMu           sync.Mutex
	stoppedServices = map[string]bool{}
)

func markServiceStopped(name string) {
	svcMu.Lock()
	stoppedServices[name] = true
	svcMu.Unlock()
}

func markServiceRestarted(name string) {
	svcMu.Lock()
	delete(stoppedServices, name)
	svcMu.Unlock()
}

// restartStoppedServices starts every service a task stopped and has not
// restarted yet.
func restartStoppedServices() {
	svcMu.Lock()
	names := make([]string, 0, len(stoppedServices))
	for n := range stoppedServices {
		names = append(names, n)
	}
	clear(stoppedServices)
	svcMu.Unlock()

	for _, n := range names {
		startService(n)
	}
}

// InstallShutdownHandler routes Ctrl+C and window-close to a clean exit.
// During a run, Ctrl+C acts like Q: the run stops safely and the summary
// still shows. Anywhere else, and for a closed window, Quiesce restores
// services and the console, then exits.
func InstallShutdownHandler() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		for s := range ch {
			// A second Ctrl+C force-quits a run that is stuck stopping.
			if v := activeView.Load(); v != nil && s == os.Interrupt && !v.isFinished() && !v.Aborted() {
				v.Abort()
				continue
			}
			emergencyExit()
		}
	}()
}

func emergencyExit() {
	restartStoppedServices()
	if v := activeView.Load(); v != nil {
		v.emergencyClose()
	} else {
		fmt.Print("\x1b[r\x1b[?25h")
	}
	RestoreConsoleMode()
	os.Exit(1)
}
