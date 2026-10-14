//go:build !windows

package xenium

import "syscall"

// FreeBytes is the space available to this user on the volume holding dir.
func FreeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
