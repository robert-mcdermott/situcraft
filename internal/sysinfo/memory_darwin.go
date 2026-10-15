package sysinfo

import (
	"encoding/binary"
	"syscall"
)

func memory() (int64, bool) {
	// hw.memsize is a 64-bit integer; Sysctl drops a trailing zero byte, which padding restores.
	v, err := syscall.Sysctl("hw.memsize")
	if err != nil || len(v) == 0 || len(v) > 8 {
		return 0, false
	}
	var b [8]byte
	copy(b[:], v)
	return int64(binary.LittleEndian.Uint64(b[:])), true
}
