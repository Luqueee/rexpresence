// Package autostart manages the macOS user LaunchAgent for rexpresence.
package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

const label = "com.rexpresence.agent"
const marker = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- Managed by rexpresence. -->\n"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// Installed reports whether the managed plist exists, not whether it is running.
func Installed() (bool, error) {
	path, err := plistPath()
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing non-regular LaunchAgent %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !bytes.HasPrefix(data, []byte(marker)) {
		return false, fmt.Errorf("refusing unrelated LaunchAgent %s", path)
	}
	return true, nil
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// stop unloads only this agent. An already-unloaded agent is harmless.
func stop() error {
	err := launchctl("bootout", domain()+"/"+label)
	if err == nil {
		return nil
	}
	check := launchctl("print", domain()+"/"+label)
	var exitErr *exec.ExitError
	if errors.As(check, &exitErr) {
		if domainErr := launchctl("print", domain()); domainErr != nil {
			return domainErr
		}
		return nil
	}
	if check != nil {
		return check
	}
	return err
}

func plist(args []string) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(marker + `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + label + `</string>
<key>ProgramArguments</key><array>` + "\n")
	writeString := func(arg string) error {
		if !utf8.ValidString(arg) {
			return errors.New("LaunchAgent value is not valid UTF-8")
		}
		for _, r := range arg {
			if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0xfffe || r == 0xffff) {
				return errors.New("LaunchAgent value contains an invalid XML character")
			}
		}
		b.WriteString("<string>")
		if err := xml.EscapeText(&b, []byte(arg)); err != nil {
			return err
		}
		b.WriteString("</string>\n")
		return nil
	}
	for _, arg := range append([]string{executable}, args...) {
		if err := writeString(arg); err != nil {
			return nil, err
		}
	}
	b.WriteString("</array>\n<key>EnvironmentVariables</key><dict><key>PATH</key>")
	if err := writeString(os.Getenv("PATH")); err != nil {
		return nil, err
	}
	b.WriteString(`</dict>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>10</integer>
</dict></plist>
`)
	return b.Bytes(), nil
}

// Install installs and starts the agent for the current graphical login.
func Install(args []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("LaunchAgents are only supported on macOS")
	}
	data, err := plist(args)
	if err != nil {
		return err
	}
	path, err := plistPath()
	if err != nil {
		return err
	}
	installed, err := Installed()
	if err != nil {
		return err
	}
	if installed {
		if err := stop(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Create a private temporary file and rename it, so launchd never reads a partial plist.
	f, err := os.CreateTemp(filepath.Dir(path), ".rexpresence-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if installed {
		err = os.Rename(f.Name(), path)
	} else {
		err = os.Link(f.Name(), path)
	}
	if err != nil {
		return err
	}
	return launchctl("bootstrap", domain(), path)
}

// Uninstall unloads and removes the managed agent, if installed.
func Uninstall() error {
	if runtime.GOOS != "darwin" {
		return errors.New("LaunchAgents are only supported on macOS")
	}
	installed, err := Installed()
	if err != nil || !installed {
		return err
	}
	if err := stop(); err != nil {
		return err
	}
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
