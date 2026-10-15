package sysinfo

import "syscall"

func memory() (int64, bool) {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0, false
	}
	return int64(info.Totalram) * int64(info.Unit), true
}
