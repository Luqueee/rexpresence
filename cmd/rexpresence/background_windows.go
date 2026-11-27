package main

import (
	"os"
	"os/exec"
	"syscall"
)

// terminate ends the watcher; Windows has no graceful signal for a detached process.
func terminate(p *os.Process) error { return p.Kill() }

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000208} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
}
