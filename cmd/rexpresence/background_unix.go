//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// terminate asks the watcher to shut down cleanly.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
