package integration_test

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"rexpresence/internal/autostart"
)

func TestLaunchAgentUnsupported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS supports LaunchAgents")
	}
	if err := autostart.Install(nil); err == nil {
		t.Fatal("Install succeeded on an unsupported platform")
	}
	if err := autostart.Uninstall(); err == nil {
		t.Fatal("Uninstall succeeded on an unsupported platform")
	}
}

func TestLaunchAgent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgents require macOS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", home)
	t.Setenv("LAUNCH_FIXTURE", home)
	t.Setenv("PRIVATE_TEST_SECRET", "should-not-be-in-plist")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$LAUNCH_FIXTURE/calls"
case "$1" in
 bootstrap) /usr/bin/touch "$LAUNCH_FIXTURE/loaded" ;;
 bootout) if [ "$FAIL_BOOTOUT" = 1 ] || [ ! -f "$LAUNCH_FIXTURE/loaded" ]; then exit 1; fi; /bin/rm "$LAUNCH_FIXTURE/loaded" ;;
 print) case "$2" in */*/*) [ -f "$LAUNCH_FIXTURE/loaded" ] ;; *) exit 0 ;; esac ;;
esac
`
	if err := os.WriteFile(filepath.Join(home, "launchctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "com.rexpresence.agent.plist")
	if installed, err := autostart.Installed(); err != nil || installed {
		t.Fatalf("initial Installed = %v, %v", installed, err)
	}
	args := []string{"-server", `literal & < > " ' $(touch /tmp/not-a-command)`, "-project=hello"}
	if err := autostart.Install(args); err != nil {
		t.Fatal(err)
	}
	if installed, err := autostart.Installed(); err != nil || !installed {
		t.Fatalf("Installed = %v, %v", installed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "should-not-be-in-plist") {
		t.Fatal("environment leaked into plist")
	}
	var values []string
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid plist XML: %v", err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"com.rexpresence.agent", executable}, args...)
	want = append(want, home)
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("plist strings = %#v, want %#v", values, want)
	}
	for _, fragment := range []string{"<key>RunAtLoad</key><true/>", "<key>SuccessfulExit</key><false/>", "<key>ThrottleInterval</key><integer>10</integer>"} {
		if !strings.Contains(string(data), fragment) {
			t.Errorf("missing %s", fragment)
		}
	}
	if err := autostart.Install([]string{"-session=new"}); err != nil {
		t.Fatal(err)
	}
	if err := autostart.Install([]string{"bad\x00argument"}); err == nil {
		t.Fatal("invalid XML argument accepted")
	}
	t.Setenv("FAIL_BOOTOUT", "1")
	if err := autostart.Uninstall(); err == nil {
		t.Fatal("uninstall ignored a failure to unload a running agent")
	}
	if installed, err := autostart.Installed(); err != nil || !installed {
		t.Fatalf("failed uninstall removed plist: %v, %v", installed, err)
	}
	t.Setenv("FAIL_BOOTOUT", "")
	if err := autostart.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if err := autostart.Uninstall(); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(home, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	wantCalls := []string{"bootstrap " + domain + " " + path, "bootout " + domain + "/com.rexpresence.agent", "bootstrap " + domain + " " + path, "bootout " + domain + "/com.rexpresence.agent", "print " + domain + "/com.rexpresence.agent", "bootout " + domain + "/com.rexpresence.agent"}
	if got := strings.Split(strings.TrimSpace(string(calls)), "\n"); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("launchctl calls = %#v, want %#v", got, wantCalls)
	}
	if err := autostart.Install(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, "loaded")); err != nil {
		t.Fatal(err)
	}
	if err := autostart.Uninstall(); err != nil {
		t.Fatalf("uninstall of unloaded agent: %v", err)
	}
	if err := os.WriteFile(path, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := autostart.Install(nil); err == nil {
		t.Fatal("overwrote unrelated plist")
	}
	if err := autostart.Uninstall(); err == nil {
		t.Fatal("removed unrelated plist")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated plist changed: %q, %v", data, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "target"), path); err != nil {
		t.Fatal(err)
	}
	if err := autostart.Install(nil); err == nil {
		t.Fatal("accepted symlink plist")
	}
}
