package sysinfo

import (
	"runtime"
	"testing"
)

func TestMemory(t *testing.T) {
	m, ok := Memory()
	if !ok {
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
			t.Fatal("no memory reported")
		}
		return
	}
	// More than 256 MB and less than 64 TB: a computer's memory.
	if m < 256<<20 || m > 64<<40 {
		t.Errorf("memory %d bytes", m)
	}
}
