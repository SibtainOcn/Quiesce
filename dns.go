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
