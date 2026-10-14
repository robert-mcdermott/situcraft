//go:build windows

package xenium

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// FreeBytes is the space available to this user on the volume holding dir.
func FreeBytes(dir string) (int64, error) {
	name, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	ok, _, callErr := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, callErr
	}
	return int64(available), nil
}
