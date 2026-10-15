//go:build !windows

package validation

import (
	"os"
	"runtime"
	"syscall"
)

// peakResident is a finished process's peak resident memory, in bytes.
func peakResident(ps *os.ProcessState) int64 {
	u, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return int64(u.Maxrss) // bytes
	}
	return int64(u.Maxrss) * 1024 // kilobytes elsewhere
}
