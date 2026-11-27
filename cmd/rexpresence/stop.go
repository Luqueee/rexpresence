package main

import (
	"fmt"
	"os"
	"time"

	"rexpresence/internal/status"
)

// stopWatchers shuts down every running watcher of this user. A macOS login
// service stays installed and starts again at the next login.
func stopWatchers() error {
	pids, err := status.PIDs()
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		fmt.Println("rexpresence is not running.")
		return nil
	}
	for _, pid := range pids {
		p, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		if err := terminate(p); err != nil {
			return fmt.Errorf("stop watcher %d: %w", pid, err)
		}
	}
	for range 50 {
		if left, err := status.PIDs(); err == nil && len(left) == 0 {
			fmt.Println("rexpresence stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("a watcher did not stop within 5 seconds")
}
