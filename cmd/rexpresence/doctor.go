package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"rexpresence/internal/discord"
	"rexpresence/internal/rex"
)

func doctor() error {
	printExecutableChecks()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	actions, rexErr := rex.PaletteStatus(ctx)
	cancel()
	switch {
	case rexErr != nil:
		fmt.Printf("Rex: %v\n", rexErr)
	case actions == 2:
		fmt.Println("Rex: both Discord palette actions are loaded on the local server.")
	default:
		fmt.Printf("Rex: %d of 2 Discord palette actions loaded. If you want the palette, run 'rexpresence setup'. If already installed, check 'rex config check' and reload the local config.\n", actions)
	}

	ipcErr := discord.CheckIPC(context.Background())
	if ipcErr != nil {
		fmt.Println("Discord: desktop IPC is unreachable. Open Discord desktop on this machine under the same user, then retry.")
	} else {
		fmt.Println("Discord: desktop IPC is reachable. No activity was sent.")
	}
	if rexErr != nil || ipcErr != nil {
		return errors.New("one or more connection checks failed")
	}
	return nil
}

func printExecutableChecks() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Println("Binary: could not locate the running executable.")
		return
	}
	fmt.Printf("Binary: running %q (%s)\n", executable, version())
	preferred, err := exec.LookPath("rexpresence")
	if err != nil {
		fmt.Println("Binary: rexpresence does not resolve from PATH. Add its directory to PATH or use its full path.")
	}

	name := "rexpresence"
	var dirs []string
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if path := standalonePath(); path != "" {
		dirs = append(dirs, filepath.Dir(path))
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/opt/rexpresence/bin", "/usr/local/opt/rexpresence/bin")
	}

	paths := []string{executable, preferred}
	for _, dir := range append(dirs, filepath.SplitList(os.Getenv("PATH"))...) {
		if dir == "" {
			continue
		}
		if path, err := filepath.Abs(filepath.Join(dir, name)); err == nil {
			if executablePath, err := exec.LookPath(path); err == nil {
				paths = append(paths, executablePath)
			}
		}
	}
	var copies []os.FileInfo
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen := false
		for _, previous := range copies {
			if os.SameFile(info, previous) {
				seen = true
				break
			}
		}
		if !seen {
			if len(copies) > 0 {
				fmt.Printf("Binary: another copy at %q\n", path)
			}
			copies = append(copies, info)
		}
	}
	if len(copies) > 1 && preferred != "" {
		fmt.Printf("Binary: PATH chooses %q. To use a different copy, run it by its full path or adjust PATH.\n", preferred)
	}
}
