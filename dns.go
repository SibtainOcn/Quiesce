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
	"os/exec"

	"golang.org/x/sys/windows"
)

var (
	moddnsapi                 = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = moddnsapi.NewProc("DnsFlushResolverCache")
)

func FlushDNS() bool {
	if procDnsFlushResolverCache.Find() == nil {
		r1, _, _ := procDnsFlushResolverCache.Call()
		if r1 != 0 {
			return true
		}
	}

	cmd := exec.Command("ipconfig", "/flushdns")
	err := cmd.Run()
	return err == nil
}
