package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// Replace stages an update beside path. It returns true only after installing it;
// an accompanying error then means the original backup was retained.
func Replace(path string, fetch func(io.Writer) (latest string, newer bool, err error)) (latest string, newer bool, err error) {
	original, err := os.Lstat(path)
	if err != nil {
		return "", false, fmt.Errorf("inspect executable: %w", err)
	}
	if !original.Mode().IsRegular() {
		return "", false, fmt.Errorf("executable is not a regular file: %s", path)
	}

	stage, err := os.CreateTemp(filepath.Dir(path), ".rexpresence-update-*")
	if err != nil {
		return "", false, fmt.Errorf("stage update: %w", err)
	}
	defer os.Remove(stage.Name())
	defer stage.Close()

	latest, newer, err = fetch(stage)
	if err != nil || !newer {
		return latest, false, err
	}
	if err = stage.Chmod(original.Mode().Perm()); err != nil {
		return latest, false, fmt.Errorf("set update permissions: %w", err)
	}
	if err = stage.Close(); err != nil {
		return latest, false, fmt.Errorf("close staged update: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return latest, false, fmt.Errorf("reinspect executable: %w", err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(original, current) || current.Mode() != original.Mode() || current.Size() != original.Size() || !current.ModTime().Equal(original.ModTime()) {
		return latest, false, fmt.Errorf("executable changed while downloading: %s", path)
	}
	if err = os.Rename(stage.Name(), path); err == nil {
		return latest, newer, nil
	}
	if runtime.GOOS != "windows" {
		return latest, false, fmt.Errorf("replace executable: %w", err)
	}

	// Windows may not replace an executable that is still mapped by a process.
	backupDir, backupErr := os.MkdirTemp(filepath.Dir(path), ".rexpresence-backup-*")
	if backupErr != nil {
		return latest, false, fmt.Errorf("replace executable: %v; create backup directory: %w", err, backupErr)
	}
	defer os.Remove(backupDir)
	backup := filepath.Join(backupDir, filepath.Base(path))
	if backupErr = os.Rename(path, backup); backupErr != nil {
		return latest, false, fmt.Errorf("cannot move the original executable; close a running watcher and retry: %w", backupErr)
	}
	if backupErr = os.Rename(stage.Name(), path); backupErr != nil {
		if restoreErr := os.Rename(backup, path); restoreErr != nil {
			return latest, false, fmt.Errorf("install update: %v; restore original: %v; original retained at %s", backupErr, restoreErr, backup)
		}
		return latest, false, fmt.Errorf("install update (original restored): %w", backupErr)
	}
	if backupErr = os.Remove(backup); backupErr != nil {
		return latest, true, fmt.Errorf("original retained at %s; you can remove it after rexpresence exits: %w", backup, backupErr)
	}
	return latest, newer, nil
}
