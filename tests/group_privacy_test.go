package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"rexpresence/internal/presence"
)

const privateSession = "session:12345678-1234-abcd-9876-123456789abc"
const otherSession = "session:12345678-1234-abcd-9876-123456789abd"

func TestGroupPrivacyStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, id := range []string{"", "session:a", "12345678-1234-abcd-9876-123456789abc", "session:12345678-1234-abcd-9876-123456789ab/", "session:123456781234-abcd-9876-123456789abc"} {
		if _, err := presence.GroupPrivate(id); err == nil {
			t.Errorf("accepted invalid ID %q", id)
		}
		if err := presence.SetGroupPrivate(id, true); err == nil {
			t.Errorf("accepted invalid ID %q for hide", id)
		}
	}
	if private, err := presence.GroupPrivate(privateSession); err != nil || private {
		t.Fatalf("new group: private=%v err=%v", private, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".rexpresence")); !os.IsNotExist(err) {
		t.Fatalf("read created directory: %v", err)
	}
	upper := "session:" + strings.ToUpper(strings.TrimPrefix(privateSession, "session:"))
	if err := presence.SetGroupPrivate(upper, true); err != nil {
		t.Fatal(err)
	}
	if private, err := presence.GroupPrivate(privateSession); err != nil || !private {
		t.Fatalf("normalized group: private=%v err=%v", private, err)
	}
	if private, err := presence.GroupPrivate(otherSession); err != nil || private {
		t.Fatalf("different group: private=%v err=%v", private, err)
	}
	marker := filepath.Join(home, ".rexpresence", "private-groups", strings.TrimPrefix(privateSession, "session:"))
	info, err := os.Stat(marker)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("marker: %v %v", info, err)
	}
	if err := presence.SetGroupPrivate(privateSession, false); err != nil {
		t.Fatal(err)
	}
	if private, err := presence.GroupPrivate(upper); err != nil || private {
		t.Fatalf("shown group: private=%v err=%v", private, err)
	}
	data, err := json.Marshal(presence.Activity{SessionID: privateSession, Details: "visible"})
	if err != nil || strings.Contains(string(data), privateSession) || strings.Contains(string(data), "SessionID") {
		t.Fatalf("session ID in snapshot: %s (%v)", data, err)
	}
}

func TestGroupPrivacyLiveAndRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := presence.SetGroupPrivate(privateSession, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *presence.Activity)
	output := make(chan *presence.Activity, 16)
	reports := make(chan string, 16)
	done := make(chan error, 1)
	go func() { done <- presence.ApplyMode(ctx, input, output, func(s string) { reports <- s }, nil) }()
	defer cancel()
	mode := func(want string) {
		t.Helper()
		select {
		case got := <-reports:
			if got != want {
				t.Fatalf("mode = %q, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for mode %q", want)
		}
	}
	activity := func(want string) {
		t.Helper()
		select {
		case got := <-output:
			if want == "" && got != nil || want != "" && (got == nil || got.Details != want) {
				t.Fatalf("activity = %#v, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for activity %q", want)
		}
	}
	mode("auto")
	activity("")
	input <- &presence.Activity{SessionID: privateSession, Details: "secret", Group: "same"}
	mode("private")
	select {
	case got := <-output:
		t.Fatalf("private entry emitted activity: %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	input <- &presence.Activity{SessionID: otherSession, Details: "public", Group: "same"}
	mode("auto")
	activity("public")
	if err := presence.SetGroupPrivate(otherSession, true); err != nil {
		t.Fatal(err)
	}
	mode("private")
	activity("")
	if err := presence.SetMode("custom", "Custom text"); err != nil {
		t.Fatal(err)
	}
	if err := presence.SetGroupPrivate(otherSession, false); err != nil {
		t.Fatal(err)
	}
	mode("custom")
	activity("Custom text")
	input <- &presence.Activity{SessionID: privateSession, Details: "renamed", Group: "new label"}
	mode("private")
	activity("")
	if err := presence.Pause(time.Hour); err != nil {
		t.Fatal(err)
	}
	mode("paused")
	if err := presence.SetGroupPrivate(privateSession, false); err != nil {
		t.Fatal(err)
	}
	input <- &presence.Activity{SessionID: privateSession, Details: "still paused"}
	select {
	case got := <-output:
		t.Fatalf("show during pause emitted %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	if err := presence.SetGroupPrivate(privateSession, true); err != nil {
		t.Fatal(err)
	}
	if err := presence.Resume(); err != nil {
		t.Fatal(err)
	}
	mode("private")
	if err := presence.SetGroupPrivate(privateSession, false); err != nil {
		t.Fatal(err)
	}
	mode("custom")
	activity("Custom text")
	if err := presence.SetGroupPrivate(privateSession, true); err != nil {
		t.Fatal(err)
	}
	mode("private")
	activity("")
	cancel()
	<-done
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	input2 := make(chan *presence.Activity, 1)
	input2 <- &presence.Activity{SessionID: privateSession, Details: "secret"}
	out2 := make(chan *presence.Activity, 2)
	done2 := make(chan error, 1)
	go func() { done2 <- presence.ApplyMode(ctx2, input2, out2, nil, nil) }()
	select {
	case got := <-out2:
		if got != nil {
			t.Fatalf("restart leaked %#v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not clear activity")
	}
	select {
	case got := <-out2:
		t.Fatalf("restart emitted private activity: %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	cancel2()
	<-done2
}

func TestGroupPrivacyUnreadableFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := presence.SetGroupPrivate(privateSession, true); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, ".rexpresence", "private-groups", strings.TrimPrefix(privateSession, "session:"))
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(marker, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := presence.GroupPrivate(privateSession); err == nil {
		t.Fatal("directory marker accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *presence.Activity, 1)
	input <- &presence.Activity{SessionID: privateSession, Details: "secret"}
	output := make(chan *presence.Activity, 2)
	done := make(chan error, 1)
	go func() { done <- presence.ApplyMode(ctx, input, output, nil, nil) }()
	select {
	case got := <-output:
		if got != nil {
			t.Fatalf("unreadable storage leaked %#v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no clear activity")
	}
	select {
	case got := <-output:
		t.Fatalf("unreadable storage emitted %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	<-done
	if runtime.GOOS != "windows" {
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, nil, 0000); err != nil {
			t.Fatal(err)
		}
		if _, err := presence.GroupPrivate(privateSession); err == nil {
			t.Fatal("unreadable marker accepted")
		}
	}
}
