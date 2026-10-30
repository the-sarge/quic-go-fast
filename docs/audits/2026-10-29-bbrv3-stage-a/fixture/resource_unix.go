//go:build !windows

package main

import (
	"runtime"
	"syscall"
)

func usage() resources {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		return resources{Error: err.Error()}
	}
	rss := uint64(r.Maxrss)
	if runtime.GOOS != "darwin" {
		rss *= 1024
	}
	return resources{CPUSeconds: float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6, PeakRSSBytes: rss}
}
