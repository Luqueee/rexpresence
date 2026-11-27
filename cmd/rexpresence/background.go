package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"rexpresence/internal/status"
)

// startBackground starts a detached watcher unless one already reports status.
func startBackground(args []string) error {
	if _, err := status.ReadSnapshot(); err == nil {
		fmt.Println("rexpresence is already running. Run 'rexpresence status' for details.")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var forward []string
	for _, a := range args {
		if a != "-d" && a != "--d" && a != "-d=true" && a != "--d=true" {
			forward = append(forward, a)
		}
	}
	cmd := exec.Command(exe, append([]string{"run"}, forward...)...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		if _, err := status.ReadSnapshot(); err == nil {
			fmt.Println("rexpresence is running in the background. Run 'rexpresence status' to check it, or 'rexpresence install' to start it at login (macOS).")
			return nil
		}
	}
	return fmt.Errorf("the watcher did not start; run 'rexpresence run' to see the error")
}
