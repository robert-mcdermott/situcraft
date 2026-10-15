//go:build windows

package validation

import "os"

// peakResident is not measured on Windows.
func peakResident(*os.ProcessState) int64 { return 0 }
