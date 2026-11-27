package integration_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rexpresence/internal/presence"
	"rexpresence/internal/status"
)

func TestStatusReflectsLiveInstanceOnly(t *testing.T) {
	// Unix socket addresses have a small platform-specific length limit.
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	live, err := status.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { live.Close() })
	live.Session("dev\nwork")
	live.Mode("custom")
	live.Discord("Awaiting Discord acknowledgement")
	var out bytes.Buffer
	if err := status.Print(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `Rex: watching "dev\nwork"`) || !strings.Contains(out.String(), "Mode: custom\n") || !strings.Contains(out.String(), "Discord: Awaiting Discord acknowledgement") {
		t.Fatal(out.String())
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := status.Print(&out); err != nil || out.String() != "Watcher: not running\n" {
		t.Fatalf("status after shutdown: %q, %v", out.String(), err)
	}
}

func TestSnapshotActivityAndPrivacy(t *testing.T) {
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	live, err := status.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	live.Mode("auto")
	card := &presence.Card{Line1: "private template"}
	activity := &presence.Activity{Details: "private activity", Group: "private group", Card: card}
	live.Activity(activity, "private logo")
	activity.Details = "changed"
	card.Line1 = "changed"
	snapshot, err := status.ReadSnapshot()
	if err != nil || snapshot.Mode != "auto" || snapshot.Logo != "private logo" || snapshot.Activity == nil || snapshot.Activity.Details != "private activity" || snapshot.Activity.Card == nil || snapshot.Activity.Card.Line1 != "private template" {
		t.Fatalf("snapshot: %#v, %v", snapshot, err)
	}
	var out bytes.Buffer
	if err := status.Print(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private") || strings.Contains(out.String(), "changed") {
		t.Fatalf("status leaked activity: %q", out.String())
	}
	live.Mode("custom")
	snapshot, err = status.ReadSnapshot()
	if err != nil || snapshot.Mode != "custom" || snapshot.Activity != nil {
		t.Fatalf("custom transition: %#v, %v", snapshot, err)
	}
	live.Activity(activity, "logo")
	snapshot, err = status.ReadSnapshot()
	if err != nil || snapshot.Activity != nil {
		t.Fatalf("custom mode exposed automatic activity: %#v, %v", snapshot, err)
	}
	live.Activity(&presence.Activity{Custom: true, Details: "private custom text"}, "logo")
	snapshot, err = status.ReadSnapshot()
	if err != nil || snapshot.Activity == nil || snapshot.Activity.Details != "private custom text" {
		t.Fatalf("custom activity: %#v, %v", snapshot, err)
	}
	out.Reset()
	if err := status.Print(&out); err != nil || strings.Contains(out.String(), "private custom text") || strings.Contains(out.String(), "logo") {
		t.Fatalf("status leaked custom activity: %q, %v", out.String(), err)
	}
	live.Mode("paused")
	snapshot, err = status.ReadSnapshot()
	if err != nil || snapshot.Mode != "paused" || snapshot.Activity != nil {
		t.Fatalf("paused transition: %#v, %v", snapshot, err)
	}
	live.Mode("auto")
	live.Activity(&presence.Activity{Custom: true, Details: "old custom"}, "logo")
	snapshot, err = status.ReadSnapshot()
	if err != nil || snapshot.Activity != nil {
		t.Fatalf("automatic mode exposed custom activity: %#v, %v", snapshot, err)
	}
}

func TestSnapshotRejectsMultipleAndStaleWatchers(t *testing.T) {
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := status.ReadSnapshot(); err == nil {
		t.Fatal("expected error without watcher")
	}
	live, err := status.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	path := filepath.Join(home, ".rexpresence", "other.sock")
	other, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := status.ReadSnapshot(); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple watcher error, got %v", err)
	}
	other.(*net.UnixListener).SetUnlinkOnClose(false)
	other.Close()
	if _, err := status.ReadSnapshot(); err != nil {
		t.Fatalf("stale socket blocked live watcher: %v", err)
	}
	live.Close()
	if _, err := status.ReadSnapshot(); err == nil {
		t.Fatal("stale socket counted as watcher")
	}
}

func TestSnapshotRejectsUnresponsiveWatcher(t *testing.T) {
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".rexpresence")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "stuck.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := status.ReadSnapshot(); err == nil {
		t.Fatal("expected unresponsive watcher error")
	}
}
