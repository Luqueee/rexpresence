package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rexpresence/internal/presence"
)

func TestModeLiveTransitions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := presence.SetMode("paused", ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, ".rexpresence", "mode"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode file permissions: %v, %v", info, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *presence.Activity)
	output := make(chan *presence.Activity, 16)
	reports := make(chan string, 16)
	done := make(chan error, 1)
	go func() { done <- presence.ApplyMode(ctx, input, output, func(s string) { reports <- s }, nil) }()
	defer func() { cancel(); <-done }()
	mode := func(want string) {
		t.Helper()
		select {
		case got := <-reports:
			if got != want {
				t.Fatalf("mode = %q, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for mode %q", want)
		}
	}
	get := func(want *presence.Activity) *presence.Activity {
		t.Helper()
		select {
		case got := <-output:
			if (got == nil) != (want == nil) || got != nil && (got.Details != want.Details || got.Group != want.Group || want.Started != 0 && got.Started != want.Started) {
				t.Fatalf("activity = %#v, want %#v", got, want)
			}
			return got
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for activity %#v", want)
			return nil
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case got := <-output:
			t.Fatalf("unexpected activity: %#v", got)
		case <-time.After(1200 * time.Millisecond):
		}
	}
	mode("paused")
	get(nil)
	input <- &presence.Activity{Details: "first", Group: "private group", Started: 10}
	quiet()
	if err := presence.SetMode("custom", "  Public text  "); err != nil {
		t.Fatal(err)
	}
	mode("custom")
	custom := get(&presence.Activity{Details: "Public text"})
	input <- &presence.Activity{Details: "second", Group: "private group", Started: 20}
	quiet()
	input <- nil
	get(nil)
	input <- &presence.Activity{Details: "third", Group: "private group", Started: 30}
	get(&presence.Activity{Details: "Public text", Started: custom.Started})
	if err := presence.SetMode("paused", ""); err != nil {
		t.Fatal(err)
	}
	mode("paused")
	get(nil)
	input <- &presence.Activity{Details: "latest", Group: "polished-sierra", Started: 40}
	quiet()
	if err := presence.SetMode("auto", ""); err != nil {
		t.Fatal(err)
	}
	mode("auto")
	get(&presence.Activity{Details: "latest", Group: "polished-sierra", Started: 40})
	if err := os.WriteFile(filepath.Join(home, ".rexpresence", "mode"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	mode("paused")
	get(nil)
	input <- &presence.Activity{Details: "new latest", Started: 50}
	quiet()
	if err := presence.SetMode("auto", ""); err != nil {
		t.Fatal(err)
	}
	mode("auto")
	get(&presence.Activity{Details: "new latest", Started: 50})
}

func TestModeValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, text := range []string{"", " ", "a", strings.Repeat("x", 129), "private\ntext", string([]byte{0xff, 'x'})} {
		if err := presence.SetMode("custom", text); err == nil || text == "private\ntext" && strings.Contains(err.Error(), text) {
			t.Fatalf("invalid text accepted or exposed: %q, %v", text, err)
		}
	}
	if err := presence.SetMode("unknown", "secret"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid mode accepted or text exposed: %v", err)
	}
	if err := presence.SetMode("custom", "界界"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, ".rexpresence", "mode"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode file permissions: %v, %v", info, err)
	}
}

func TestModeInvalidStartupFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".rexpresence")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode"), []byte(strings.Repeat("x", 4097)), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *presence.Activity)
	output := make(chan *presence.Activity, 4)
	reports := make(chan string, 4)
	done := make(chan error, 1)
	go func() { done <- presence.ApplyMode(ctx, input, output, func(s string) { reports <- s }, nil) }()
	defer func() { cancel(); <-done }()
	select {
	case got := <-reports:
		if got != "paused" {
			t.Fatalf("mode = %q, want paused", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no mode reported")
	}
	input <- &presence.Activity{Details: "must remain private", Started: 1}
	select {
	case got := <-output:
		if got != nil {
			t.Fatalf("published activity with invalid mode: %#v", got)
		}
	case <-time.After(1200 * time.Millisecond):
	}
	select {
	case got := <-output:
		t.Fatalf("published activity with invalid mode: %#v", got)
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestModeRejectsUnsafeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	outside := t.TempDir()
	dir := filepath.Join(home, ".rexpresence")
	if err := os.Symlink(outside, dir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := presence.SetMode("custom", "public"); err == nil {
		t.Fatal("accepted symlinked directory")
	}
	if err := presence.SetMode("auto", ""); err == nil {
		t.Fatal("accepted symlinked directory for auto")
	}
	if _, err := os.Stat(filepath.Join(outside, "mode")); !os.IsNotExist(err) {
		t.Fatalf("wrote outside home: %v", err)
	}
}

func TestModeInitiallyClearsWhenPaused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := presence.SetMode("paused", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *presence.Activity)
	output := make(chan *presence.Activity)
	done := make(chan error, 1)
	go func() { done <- presence.ApplyMode(ctx, input, output, nil, nil) }()
	defer func() { cancel(); <-done }()
	input <- &presence.Activity{Details: "private", Started: 1}
	select {
	case got := <-output:
		if got != nil {
			t.Fatalf("published while paused: %#v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initial clear was lost")
	}
}
