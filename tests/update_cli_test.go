package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionAndUpdateCLI(t *testing.T) {
	dir := t.TempDir()
	name := "rexpresence"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	build := func(ldflags string) {
		t.Helper()
		out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "../cmd/rexpresence").CombinedOutput()
		if err != nil {
			t.Fatalf("build: %v: %s", err, out)
		}
	}
	version := func() string {
		t.Helper()
		out, err := exec.Command(bin, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("version: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	assertUpdateRefused := func(message string) {
		t.Helper()
		out, err := exec.Command(bin, "update").CombinedOutput()
		if err == nil || !strings.Contains(string(out), message) {
			t.Fatalf("update: %v: %s", err, out)
		}
	}

	build("")
	if got := version(); !strings.HasPrefix(got, "rexpresence devel") {
		t.Fatalf("source version = %q", got)
	}
	assertUpdateRefused("development build")

	build("-X main.releaseVersion=v0.4.0")
	if got := version(); got != "rexpresence v0.4.0" {
		t.Fatalf("version = %q", got)
	}
	assertUpdateRefused("not at the standalone installer's location")

	if runtime.GOOS == "windows" {
		return
	}
	brew := filepath.Join(dir, "bin", "brew")
	if err := os.Mkdir(filepath.Dir(brew), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brew, []byte("#!/bin/sh\nprintf 'brew %s\\n' \"$*\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	paletteExecutable := filepath.Join(dir, "opt", "rexpresence", "bin", "rexpresence")
	build(`-X main.releaseVersion=v0.4.0 -X "main.paletteExecutable=` + paletteExecutable + `"`)
	out, err := exec.Command(bin, "update").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "brew upgrade luqueee/tap/rexpresence\n") {
		t.Fatalf("Homebrew update: %v: %s", err, out)
	}
}
