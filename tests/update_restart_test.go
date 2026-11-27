//go:build !windows

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rexpresence/internal/status"
)

func TestUpdateRestartsRunningWatcher(t *testing.T) {
	// Unix socket addresses have a small platform-specific length limit.
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	opt := filepath.Join(home, "opt", "rexpresence", "bin", "rexpresence")
	brew := filepath.Join(home, "bin", "brew")
	restarted := filepath.Join(home, "restarted")
	for path, script := range map[string]string{
		brew: "#!/bin/sh\nprintf 'brew %s\\n' \"$*\"\n",
		opt:  "#!/bin/sh\nprintf '%s' \"$*\" > '" + restarted + "'\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(home, "rexpresence")
	ldflags := `-X main.releaseVersion=v0.4.0 -X "main.paletteExecutable=` + opt + `"`
	if out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "../cmd/rexpresence").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	// Without a running watcher, nothing is started.
	out, err := exec.Command(bin, "upgrade").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "brew upgrade luqueee/tap/rexpresence") {
		t.Fatalf("upgrade: %v: %s", err, out)
	}
	if _, err := os.Stat(restarted); !os.IsNotExist(err) {
		t.Fatalf("started a watcher that was not running: %v", err)
	}

	helper := exec.Command(os.Args[0], "-test.run=^TestStopHelperWatcher$")
	helper.Env = append(os.Environ(), "REXPRESENCE_STOP_HELPER=1")
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { helper.Wait(); close(done) }()
	t.Cleanup(func() { helper.Process.Kill() })
	deadline := time.Now().Add(5 * time.Second)
	for pids, _ := status.PIDs(); len(pids) == 0; pids, _ = status.PIDs() {
		if time.Now().After(deadline) {
			t.Fatal("helper watcher did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	out, err = exec.Command(bin, "update").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Restarted") {
		t.Fatalf("update: %v: %s", err, out)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old watcher survived the update")
	}
	if got, err := os.ReadFile(restarted); err != nil || string(got) != "-d" {
		t.Fatalf("restart arguments = %q, %v", got, err)
	}
}
