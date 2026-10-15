//go:build !darwin && !linux && !windows

package sysinfo

func memory() (int64, bool) { return 0, false }
