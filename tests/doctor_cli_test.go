//go:build !windows

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCLI(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "rp-doctor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	standalone := filepath.Join(root, ".local", "bin")
	other := filepath.Join(root, "brew", "bin")
	for _, dir := range []string{standalone, other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(standalone, "rexpresence")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/rexpresence").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	alias := filepath.Join(root, "rexpresence")
	if err := os.Symlink(bin, alias); err != nil {
		t.Fatal(err)
	}
	duplicate := filepath.Join(other, "rexpresence")
	if err := os.WriteFile(duplicate, []byte("#!/bin/sh\n: > \"$HOME/duplicate-ran\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `#!/bin/sh
case " $* " in *" --autostart=false "*) ;; *) exit 1 ;; esac
case " $* " in *" actions --json "*) ;; *) exit 1 ;; esac
if [ "$DOCTOR_REX_FAIL" = 1 ]; then exit 1; fi
printf '%s\n' '{"actions":[{"name":"discord_change_presence"}]}'
`
	if err := os.WriteFile(filepath.Join(root, "rex"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	listener := ipcListen(t, filepath.Join(root, "discord-ipc-0"))
	t.Setenv("HOME", root)
	t.Setenv("XDG_RUNTIME_DIR", root)
	t.Setenv("PATH", strings.Join([]string{other, root, standalone}, string(os.PathListSeparator)))

	out, err := exec.Command(bin, "doctor").CombinedOutput()
	if err != nil {
		t.Fatalf("doctor: %v: %s", err, out)
	}
	got := string(out)
	for _, text := range []string{duplicate, "PATH chooses", "1 of 2", "rexpresence setup", "desktop IPC is reachable"} {
		if !strings.Contains(got, text) {
			t.Errorf("doctor report missing %q: %s", text, got)
		}
	}
	if strings.Count(got, "another copy") != 1 || strings.Contains(got, `another copy at "`+alias+`"`) {
		t.Errorf("symlink counted as separate copy: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "duplicate-ran")); !os.IsNotExist(err) {
		t.Fatalf("doctor executed a candidate binary: %v", err)
	}
	ipcAccept(t, listener)

	t.Setenv("DOCTOR_REX_FAIL", "1")
	out, err = exec.Command(bin, "doctor").CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "desktop IPC is reachable") {
		t.Fatalf("doctor did not report Discord after the Rex check failed: %v: %s", err, out)
	}
}
