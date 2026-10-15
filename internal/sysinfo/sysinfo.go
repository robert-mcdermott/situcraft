// Package sysinfo says what the computer has: its memory (for a job's preflight, which compares
// what a job would need with what there is).
package sysinfo

// Memory is the computer's physical memory in bytes, when the platform says (ok false otherwise).
func Memory() (bytes int64, ok bool) { return memory() }
