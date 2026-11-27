package presence

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func groupPath(id string) (string, error) {
	if len(id) != len("session:")+36 || !strings.HasPrefix(id, "session:") {
		return "", errors.New("invalid session ID")
	}
	uuid := strings.ToLower(id[len("session:"):])
	for i, c := range uuid {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", errors.New("invalid session ID")
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", errors.New("invalid session ID")
		}
	}
	path, err := modePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "private-groups", uuid), nil
}

// GroupPrivate reports whether a Rex session is private without creating storage.
func GroupPrivate(id string) (bool, error) {
	path, err := groupPath(id)
	if err != nil {
		return false, err
	}
	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		if err := privateDirectory(dir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0400 == 0) {
		return false, errors.New("invalid private group marker")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	var b [1]byte
	_, err = file.Read(b[:])
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetGroupPrivate changes whether a Rex session may be published.
func SetGroupPrivate(id string, private bool) error {
	path, err := groupPath(id)
	if err != nil {
		return err
	}
	exists, err := GroupPrivate(id)
	if err != nil {
		return err
	}
	if exists == private {
		return nil
	}
	if !private {
		return os.Remove(path)
	}
	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := privateDirectory(dir); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}
