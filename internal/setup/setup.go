package setup

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type fileState struct {
	data []byte
	info os.FileInfo
}

func inspect(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	if !info.Mode().IsRegular() {
		return fileState{}, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileState{}, err
	}
	return fileState{data, info}, nil
}

func unchanged(path string, old fileState) error {
	current, err := inspect(path)
	if err != nil {
		return err
	}
	if (old.info == nil) != (current.info == nil) || old.info != nil &&
		(!os.SameFile(old.info, current.info) || old.info.Mode() != current.info.Mode() || !bytes.Equal(old.data, current.data)) {
		return fmt.Errorf("%s changed during setup", path)
	}
	return nil
}

// atomicWrite creates the replacement beside its destination before renaming it.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".rexpresence-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode.Perm()); err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// luaTokens ignores comments and long strings so their contents cannot mimic a require.
func luaTokens(src []byte) []string {
	var tokens []string
	for i := 0; i < len(src); {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '-' && i+1 < len(src) && src[i+1] == '-' {
			i += 2
			if end, ok := longStringEnd(src, i); ok {
				i = end
			} else {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			}
			continue
		}
		if end, ok := longStringEnd(src, i); ok {
			tokens = append(tokens, string(src[i:end]))
			i = end
			continue
		}
		if c == '\'' || c == '"' {
			start := i
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				if src[i] == c {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, string(src[start:i]))
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := i
			i++
			for i < len(src) && (src[i] >= 'a' && src[i] <= 'z' || src[i] >= 'A' && src[i] <= 'Z' || src[i] >= '0' && src[i] <= '9' || src[i] == '_') {
				i++
			}
			tokens = append(tokens, string(src[start:i]))
			continue
		}
		tokens = append(tokens, string(c))
		i++
	}
	return tokens
}

func longStringEnd(src []byte, start int) (int, bool) {
	if start >= len(src) || src[start] != '[' {
		return 0, false
	}
	i := start + 1
	for i < len(src) && src[i] == '=' {
		i++
	}
	if i >= len(src) || src[i] != '[' {
		return 0, false
	}
	close := "]" + strings.Repeat("=", i-start-1) + "]"
	if end := bytes.Index(src[i+1:], []byte(close)); end >= 0 {
		return i + 1 + end + len(close), true
	}
	return len(src), true
}

func hasRequire(src []byte) bool {
	// A Unix shebang is ignored by Lua but is not a Lua comment.
	s := bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	if bytes.HasPrefix(s, []byte("#!")) {
		if n := bytes.IndexByte(s, '\n'); n >= 0 {
			s = s[n+1:]
		} else {
			s = nil
		}
	}
	t := luaTokens(s)
	for i := range t {
		if t[i] != "require" || i > 0 && (t[i-1] == "." || t[i-1] == ":") {
			continue
		}
		j := i + 1
		paren := j < len(t) && t[j] == "("
		if paren {
			j++
		}
		if j < len(t) && (t[j] == `"rexpresence"` || t[j] == `'rexpresence'` || t[j] == `[[rexpresence]]`) && (!paren || j+1 < len(t) && t[j+1] == ")") {
			return true
		}
	}
	return false
}

func addRequire(src []byte) []byte {
	newline := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		newline = "\r\n"
	}
	pos := 0
	if bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
		pos = 3
	}
	prefix := append([]byte(nil), src[:pos]...)
	if bytes.HasPrefix(src[pos:], []byte("#!")) {
		if n := bytes.IndexByte(src[pos:], '\n'); n >= 0 {
			pos += n + 1
			prefix = append(prefix, src[len(prefix):pos]...)
		} else {
			prefix = append(prefix, src[pos:]...)
			prefix = append(prefix, newline...)
			pos = len(src)
		}
	}
	prefix = append(prefix, []byte("require(\"rexpresence\")"+newline)...)
	return append(prefix, src[pos:]...)
}

// Install installs the module and require after explicit consent, returning whether files changed.
func Install(initPath string, module []byte, in io.Reader, out io.Writer, validate func(string) error) (bool, error) {
	modulePath := filepath.Join(filepath.Dir(initPath), "rexpresence.lua")
	initAbs, err := filepath.Abs(initPath)
	if err != nil {
		return false, err
	}
	moduleAbs, err := filepath.Abs(modulePath)
	if err != nil {
		return false, err
	}
	if initAbs == moduleAbs {
		return false, fmt.Errorf("init and module paths collide: %s", initPath)
	}
	initial, err := inspect(initPath)
	if err != nil {
		return false, err
	}
	installed, err := inspect(modulePath)
	if err != nil {
		return false, err
	}
	if initial.info != nil && installed.info != nil && os.SameFile(initial.info, installed.info) {
		return false, fmt.Errorf("init and module paths refer to the same file")
	}
	newInit := initial.data
	if !hasRequire(newInit) {
		newInit = addRequire(newInit)
	}
	changeInit := initial.info == nil || !bytes.Equal(initial.data, newInit)
	changeModule := installed.info == nil || !bytes.Equal(installed.data, module)
	if !changeInit && !changeModule {
		_, err = fmt.Fprintln(out, "Already installed; no changes needed.")
		return false, err
	}
	backup := ""
	dir := filepath.Dir(initPath)
	_, dirErr := os.Stat(dir)
	if dirErr != nil && !errors.Is(dirErr, os.ErrNotExist) {
		return false, dirErr
	}
	if errors.Is(dirErr, os.ErrNotExist) {
		if _, err = fmt.Fprintf(out, "Create directory %s (and missing parents)\n", dir); err != nil {
			return false, err
		}
	}
	if changeModule {
		action := "Create"
		if installed.info != nil {
			action = "Replace"
			for n := 0; ; n++ {
				backup = modulePath + ".bak"
				if n > 0 {
					backup += fmt.Sprintf(".%d", n)
				}
				_, e := os.Lstat(backup)
				if errors.Is(e, os.ErrNotExist) {
					break
				}
				if e != nil {
					return false, e
				}
			}
		}
		if _, err = fmt.Fprintf(out, "%s %s\n", action, modulePath); err != nil {
			return false, err
		}
		if backup != "" {
			if _, err = fmt.Fprintf(out, "Create backup %s\n", backup); err != nil {
				return false, err
			}
		}
	}
	if changeInit {
		action := "Update"
		if initial.info == nil {
			action = "Create"
		}
		if _, err = fmt.Fprintf(out, "%s %s (add require)\n", action, initPath); err != nil {
			return false, err
		}
	}
	if _, err = fmt.Fprint(out, "Proceed? [y/N] "); err != nil {
		return false, err
	}
	answer, readErr := bufio.NewReader(in).ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return false, readErr
	}
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return false, nil
	}
	if err = unchanged(initPath, initial); err != nil {
		return false, err
	}
	if err = unchanged(modulePath, installed); err != nil {
		return false, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}

	var backupState fileState
	if backup != "" {
		f, e := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, installed.info.Mode().Perm())
		if e != nil {
			return false, e
		}
		_, e = f.Write(installed.data)
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			os.Remove(backup)
			return false, e
		}
		backupState, e = inspect(backup)
		if e != nil {
			return false, e
		}
	}
	var writtenState fileState
	moduleWritten := false
	rollback := func(cause error) error {
		if moduleWritten {
			current, e := inspect(modulePath)
			if e != nil || current.info == nil || writtenState.info == nil || !os.SameFile(writtenState.info, current.info) || !bytes.Equal(current.data, module) {
				return errors.Join(cause, fmt.Errorf("cannot safely restore %s; it changed after installation", modulePath))
			}
			if installed.info == nil {
				e = os.Remove(modulePath)
			} else {
				e = atomicWrite(modulePath, installed.data, installed.info.Mode())
			}
			if e != nil {
				return errors.Join(cause, fmt.Errorf("restoring %s: %w", modulePath, e))
			}
		}
		if backup != "" {
			if e := unchanged(backup, backupState); e != nil {
				return errors.Join(cause, e)
			}
			if e := os.Remove(backup); e != nil {
				return errors.Join(cause, e)
			}
		}
		return cause
	}
	if changeModule {
		if err = unchanged(modulePath, installed); err != nil {
			return false, rollback(err)
		}
		mode := os.FileMode(0600)
		if installed.info != nil {
			mode = installed.info.Mode()
		}
		if err = atomicWrite(modulePath, module, mode); err != nil {
			return false, rollback(err)
		}
		moduleWritten = true
		writtenState, err = inspect(modulePath)
		if err != nil {
			return false, rollback(err)
		}
	}
	if err = unchanged(initPath, initial); err != nil {
		return false, rollback(err)
	}
	if validate != nil {
		f, e := os.CreateTemp(filepath.Dir(initPath), ".rexpresence-init-*.lua")
		if e != nil {
			return false, rollback(e)
		}
		name := f.Name()
		_, e = f.Write(newInit)
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e == nil {
			e = validate(name)
		}
		if removeErr := os.Remove(name); e == nil {
			e = removeErr
		}
		if e != nil {
			return false, rollback(fmt.Errorf("validating configuration: %w", e))
		}
	}
	if moduleWritten {
		if err = unchanged(modulePath, writtenState); err != nil {
			return false, rollback(err)
		}
	} else if err = unchanged(modulePath, installed); err != nil {
		return false, rollback(err)
	}
	if err = unchanged(initPath, initial); err != nil {
		return false, rollback(err)
	}
	if changeInit {
		mode := os.FileMode(0600)
		if initial.info != nil {
			mode = initial.info.Mode()
		}
		if err = atomicWrite(initPath, newInit, mode); err != nil {
			return false, rollback(err)
		}
	}
	return true, nil
}
