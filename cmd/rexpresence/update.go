package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"rexpresence/internal/update"
)

const releaseOrigin = "https://github.com/Luqueee/rexpresence/releases"

// releaseVersion is set by the release build and the Homebrew formula.
var releaseVersion string

func version() string {
	if releaseVersion != "" {
		return releaseVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 12 {
				return "devel (" + setting.Value[:12] + ")"
			}
		}
	}
	return "devel"
}

func standalonePath() string {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "Programs", "rexpresence", "rexpresence.exe")
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "bin", "rexpresence")
	}
	return ""
}

func updateCommand() error {
	if paletteExecutable != "" {
		brew := filepath.Join(filepath.Dir(paletteExecutable), "..", "..", "..", "bin", "brew")
		cmd := exec.Command(brew, "upgrade", "luqueee/tap/rexpresence")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Homebrew upgrade failed; try 'brew upgrade luqueee/tap/rexpresence': %w", err)
		}
		fmt.Println("Restart rexpresence if it was running to use the updated binary.")
		return nil
	}
	if releaseVersion == "" {
		return errors.New("cannot update a development build; rebuild it from source or install a GitHub release")
	}
	path := standalonePath()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	realExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if path == "" || err != nil || (realExe != realPath && !(runtime.GOOS == "windows" && strings.EqualFold(realExe, realPath))) {
		return fmt.Errorf("this binary is not at the standalone installer's location; replace it manually from %s/latest", releaseOrigin)
	}

	asset := "rexpresence-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	latest, updated, err := update.Replace(path, func(dst io.Writer) (string, bool, error) {
		return update.Download(ctx, http.DefaultClient, releaseOrigin, releaseVersion, asset, dst)
	})
	if err != nil && !updated {
		return err
	}
	if !updated {
		fmt.Printf("Already up to date: %s (latest stable: %s).\n", releaseVersion, latest)
		return nil
	}
	fmt.Printf("Updated rexpresence from %s to %s. Restart rexpresence if it was running.\n", releaseVersion, latest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning:", err)
	}
	return nil
}
