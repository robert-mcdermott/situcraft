//go:build !linux

package main

import "os/exec"

// browserProcess: elsewhere than Linux, a browser is ended by SituCraft as it stops.
func browserProcess(*exec.Cmd) {}

// browserFlags: none are needed elsewhere than Linux's containers.
func browserFlags() []string { return nil }
