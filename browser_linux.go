//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// browserProcess makes the headless browser end with SituCraft: a run its scheduler stops (or
// kills) leaves no browser behind.
func browserProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// browserFlags are what a container needs: its /dev/shm is often too small for a browser's shared
// memory, and as root Chrome's sandbox cannot start.
func browserFlags() []string {
	flags := []string{"--disable-dev-shm-usage"}
	if os.Geteuid() == 0 {
		flags = append(flags, "--no-sandbox")
	}
	return flags
}
