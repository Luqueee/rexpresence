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

// Runs as a child process: a stand-in watcher that serves status until it is terminated.
func TestStopHelperWatcher(t *testing.T) {
	if os.Getenv("REXPRESENCE_STOP_HELPER") != "1" {
		t.Skip("helper process")
	}
	live, err := status.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	time.Sleep(time.Minute)
}

func TestStopEndsRunningWatcher(t *testing.T) {
	// Unix socket addresses have a small platform-specific length limit.
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "rexpresence")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/rexpresence").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	stop := func() string {
		out, err := exec.Command(bin, "stop").CombinedOutput()
		if err != nil {
			t.Fatalf("stop: %v: %s", err, out)
		}
		return string(out)
	}
	if out := stop(); !strings.Contains(out, "not running") {
		t.Fatalf("stop without watcher: %q", out)
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

	if out := stop(); !strings.Contains(out, "stopped") {
		t.Fatalf("stop: %q", out)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher process survived stop")
	}
}
