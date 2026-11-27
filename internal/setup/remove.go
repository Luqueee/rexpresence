package setup

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func moduleHeader(data []byte) []byte {
	return fmt.Appendf(nil, "-- rexpresence setup: %x\n", sha256.Sum256(data))
}

func isManagedModule(data []byte) bool {
	end := bytes.IndexByte(data, '\n') + 1
	return end > 0 && bytes.Equal(data[:end], moduleHeader(data[end:]))
}

// Remove unhooks the managed require and deletes the module only if setup created it and it is unchanged.
func Remove(initPath string, in io.Reader, out io.Writer, validate func(string) error) (bool, error) {
	initial, err := inspect(initPath)
	if err != nil {
		return false, err
	}
	pos := configStart(initial.data)
	line, rest, _ := bytes.Cut(initial.data[pos:], []byte("\n"))
	dir := filepath.Dir(initPath)
	modulePath := filepath.Join(dir, "rexpresence.lua")
	if string(bytes.TrimSuffix(line, []byte("\r"))) != managedRequire {
		if _, err := os.Lstat(modulePath); err == nil || hasRequire(initial.data) {
			_, err := fmt.Fprintln(out, "No managed require found; older or manual palette installations must be removed from the Rex config by hand.")
			return false, err
		} else if !os.IsNotExist(err) {
			return false, err
		}
		_, err := fmt.Fprintln(out, "No managed palette to remove.")
		return false, err
	}
	newInit := append(bytes.Clone(initial.data[:pos]), rest...)
	if hasRequire(newInit) {
		return false, fmt.Errorf("%s contains another require for rexpresence; no files were changed", initPath)
	}
	module, err := inspect(modulePath)
	if err != nil {
		return false, err
	}
	removeModule := isManagedModule(module.data)
	if _, err := fmt.Fprintf(out, "Update %s (remove managed require)\n", initPath); err != nil {
		return false, err
	}
	if module.info != nil {
		action := "Keep (edited or not managed)"
		if removeModule {
			action = "Remove"
		}
		if _, err := fmt.Fprintf(out, "%s %s\n", action, modulePath); err != nil {
			return false, err
		}
	}
	if ok, err := confirm(in, out); err != nil || !ok {
		return false, err
	}
	if err := validateDraft(dir, newInit, validate); err != nil {
		return false, err
	}
	if err := unchanged(initPath, initial); err != nil {
		return false, err
	}
	if err := unchanged(modulePath, module); err != nil {
		return false, err
	}
	if err := atomicWrite(initPath, newInit, initial.info.Mode()); err != nil {
		return false, err
	}
	if removeModule {
		if err := unchanged(modulePath, module); err != nil {
			return true, err
		}
		if err := os.Remove(modulePath); err != nil {
			return true, fmt.Errorf("palette unhooked from Rex, but the module was left in place: %w", err)
		}
	}
	return true, nil
}
