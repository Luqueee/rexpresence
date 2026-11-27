package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rexpresence/internal/rex"
)

func TestRexConfigForSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture requires a Unix shell")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf 'call\n' >> "$SETUP_LOG"
printf 'arg=%s\n' "$@" >> "$SETUP_LOG"
printf 'server=%s\nhome=%s\n' "$REX_SERVER" "$REX_HOME" >> "$SETUP_LOG"
if [ "$SETUP_FAIL" = 1 ]; then echo 'private config contents' >&2; exit 1; fi
if [ "$3" = check ]; then
  if [ "$SETUP_MISSING" = all ]; then
    printf '%s\n' '{"actions":[{"name":"other"}]}'
  elif [ "$SETUP_MISSING" = 1 ]; then
    printf '%s\n' '{"actions":[{"name":"discord_change_presence"}]}'
  else
    printf '%s\n' '{"actions":[{"name":"discord_change_presence"},{"name":"discord_configure_card"}]}'
  fi
fi
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SETUP_LOG", log)
	t.Setenv("REX_CONFIG", "")
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("REX_SERVER", "remote.example:1234")
	t.Setenv("REX_HOME", "remote.example:1234")

	path, validate, reload, err := rex.ConfigForSetup(false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "home", ".config", "rex", "init.lua"); path != want {
		t.Fatalf("default path = %q, want %q", path, want)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("discovery ran Rex: %v", err)
	}
	file := filepath.Join(dir, "config with spaces.lua")
	if err := validate(file); err != nil {
		t.Fatal(err)
	}
	if err := reload(); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "call\narg=--autostart=false\narg=config\narg=check\narg=--json\narg=--\narg=" + file + "\nserver=remote.example:1234\nhome=remote.example:1234\n" +
		"call\narg=--autostart=false\narg=config\narg=reload\nserver=\nhome=\n"
	if string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	path, _, _, err = rex.ConfigForSetup(false)
	if err != nil || path != filepath.Join(dir, "xdg", "rex", "init.lua") {
		t.Fatalf("XDG path = %q, error = %v", path, err)
	}
	t.Setenv("REX_CONFIG", filepath.Join(dir, "custom.lua"))
	path, validate, reload, err = rex.ConfigForSetup(false)
	if err != nil || path != filepath.Join(dir, "custom.lua") {
		t.Fatalf("override path = %q, error = %v", path, err)
	}
	t.Setenv("SETUP_MISSING", "1")
	if err := validate(file); err == nil || !strings.Contains(err.Error(), "require(\"rexpresence\")") {
		t.Fatalf("missing action error = %v", err)
	}
	_, validateRemoval, _, err := rex.ConfigForSetup(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRemoval(file); err == nil || !strings.Contains(err.Error(), "still loads") {
		t.Fatalf("remaining action error = %v", err)
	}
	t.Setenv("SETUP_MISSING", "all")
	if err := validateRemoval(file); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETUP_MISSING", "")
	t.Setenv("SETUP_FAIL", "1")
	for name, run := range map[string]func() error{
		"check":  func() error { return validate(file) },
		"reload": reload,
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "private config contents") {
			t.Errorf("%s error = %v", name, err)
		}
	}
}

func TestRexConfigForSetupWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows config paths")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rex.exe"), []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("REX_CONFIG", "")
	t.Setenv("APPDATA", filepath.Join(dir, "roaming"))
	path, _, _, err := rex.ConfigForSetup(false)
	if err != nil || path != filepath.Join(dir, "roaming", "rex", "init.lua") {
		t.Fatalf("AppData path = %q, error = %v", path, err)
	}
	t.Setenv("REX_CONFIG", filepath.Join(dir, "custom.lua"))
	path, _, _, err = rex.ConfigForSetup(false)
	if err != nil || path != filepath.Join(dir, "custom.lua") {
		t.Fatalf("override path = %q, error = %v", path, err)
	}
	t.Setenv("REX_CONFIG", "")
	t.Setenv("APPDATA", "")
	if _, _, _, err = rex.ConfigForSetup(false); err == nil {
		t.Fatal("missing APPDATA accepted")
	}
}
