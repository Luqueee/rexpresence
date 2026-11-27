package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"rexpresence/internal/presence"
	"rexpresence/internal/rex"
)

func TestRexWatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture requires a Unix shell and FIFO")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "events")
	if err := exec.Command("mkfifo", fifo).Run(); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ "$1" = "-S" ] && [ "$2" = "remote" ] || exit 1
shift 2
case "$1 $2" in
  'session inspect') cat "$REX_FIXTURE/snapshot" ;;
  'block call') shift 2; while [ "$1" != "-b" ]; do shift; done; echo "$2" >> "$REX_FIXTURE/calls"; cat "$REX_FIXTURE/process-$2" ;;
  'events -s') exec cat "$REX_FIXTURE/events" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REX_FIXTURE", dir)
	put := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func(block, clients string) {
		put("snapshot", fmt.Sprintf(`{"session_id":"s","label":"Resolved session","active_window_id":"w","clients":[%s],"windows":[{"window_id":"w","focused_block_id":%q}]}`, clients, block))
	}
	connected := `{"connection_state":"connected","info":{"kind":"app"}}`
	snapshot("a", connected)
	put("process-a", `{"foreground":{"name":"nvim","cwd":"/private/secret"}}`)
	put("process-b", `{"foreground":{"name":"bash","cwd":"/private/secret"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan *presence.Activity, 8)
	done := make(chan error, 1)
	reported := make(chan string, 1)
	go func() {
		done <- rex.Watch(ctx, "remote", "", "public", updates, func(name string) {
			select {
			case reported <- name:
			default:
			}
		})
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watcher did not stop")
		}
	}()
	get := func(want string) *presence.Activity {
		t.Helper()
		select {
		case p := <-updates:
			if want == "" {
				if p != nil {
					t.Fatalf("wanted no activity, got %#v", p)
				}
				return nil
			}
			if p == nil || p.Details != want || p.SessionID != "s" || p.Project != "public" || p.Started == 0 {
				t.Fatalf("activity = %#v, want %q", p, want)
			}
			return p
		case <-time.After(5 * time.Second):
			t.Fatalf("missing activity %q", want)
			return nil
		}
	}
	if p := get("Neovim"); p.Group != "Resolved session" {
		t.Fatalf("group = %q", p.Group)
	}
	select {
	case name := <-reported:
		if name != "Resolved session" {
			t.Fatalf("reported session = %q", name)
		}
	default:
		t.Fatal("resolved session was not reported")
	}
	writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	send := func(typ, payload string) {
		t.Helper()
		_, err := fmt.Fprintf(writer, "{\"type\":\"session_event\",\"payload\":{\"session_id\":\"s\",\"event\":{\"type\":%q,\"payload\":%s}}}\n", typ, payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	// A background process event must not change the focused activity.
	send("block_event", `{"block_id":"b","creator_name":"com.superlogical.terminal","name":"process_changed"}`)
	snapshot("b", connected)
	send("active_window_changed", `{}`)
	get("Shell")
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || string(calls) != "a\nb\n" {
		t.Fatalf("background event triggered a query: %q, %v", calls, err)
	}
	put("process-b", `{"foreground":{"name":"private command --secret","cwd":"/private/secret"}}`)
	send("block_event", `{"block_id":"b","creator_name":"com.superlogical.terminal","name":"process_changed"}`)
	get("Terminal")
	snapshot("b", "")
	send("client_disconnected", `{"client_id":"ui"}`)
	get("")
}

func TestRexWatchFollowsMacFocus(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS PlistBuddy")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REX_FIXTURE", dir)
	script := `#!/bin/sh
case "$1 $2" in
  'session inspect') [ "$3" != "--json" ] || exit 1; cat "$REX_FIXTURE/$3" ;;
  'block call') echo '{"foreground":{"name":"nvim"}}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	preferences := filepath.Join(dir, "Library", "Preferences")
	if err := os.MkdirAll(preferences, 0700); err != nil {
		t.Fatal(err)
	}
	focus := func(id string) {
		t.Helper()
		plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Workspace.lastFocusedSession</key><string>{"sessionID":"` + id + `"}</string></dict></plist>`
		if err := os.WriteFile(filepath.Join(preferences, "com.superlogical.rex.plist"), []byte(plist), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func(id, group, clients string) {
		t.Helper()
		data := fmt.Sprintf(`{"session_id":%q,"label":%q,"active_window_id":"w","clients":[%s],"windows":[{"window_id":"w","focused_block_id":"b"}]}`, id, group, clients)
		if err := os.WriteFile(filepath.Join(dir, id), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	connected := `{"connection_state":"connected","info":{"kind":"app"}}`
	snapshot("session:a", "first", connected)
	snapshot("session:b", "first", connected)
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan *presence.Activity, 8)
	done := make(chan error, 1)
	reported := make(chan string, 32)
	go func() {
		done <- rex.Watch(ctx, "", "", "", updates, func(name string) { reported <- name })
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watcher did not stop")
		}
	}()
	get := func(group string) *presence.Activity {
		t.Helper()
		select {
		case p := <-updates:
			if group == "" {
				if p != nil {
					t.Fatalf("wanted no activity, got %#v", p)
				}
				return nil
			}
			if p == nil || p.Group != group || p.Details != "Neovim" || p.Started == 0 {
				t.Fatalf("activity = %#v, want group %q", p, group)
			}
			return p
		case err := <-done:
			t.Fatalf("watcher ended: %v", err)
		case <-time.After(7 * time.Second):
			t.Fatalf("missing group %q", group)
		}
		return nil
	}
	get("")
	focus("session:a")
	first := get("first")
	focus("session:b")
	second := get("first")
	if first.SessionID != "session:a" || second.SessionID != "session:b" {
		t.Fatalf("same-label switch did not update session identity: %q → %q", first.SessionID, second.SessionID)
	}
	if second.Started != first.Started {
		t.Fatal("switching groups reset the tool timer")
	}
	snapshot("session:b", "renamed", connected)
	renamed := get("renamed")
	if renamed.Started != first.Started {
		t.Fatal("renaming the group reset the tool timer")
	}
	focus("session:stale")
	get("")
	focus("session:b")
	get("renamed")
	snapshot("session:b", "renamed", "")
	get("")
	snapshot("session:b", "renamed", connected)
	get("renamed")
	found := false
	for len(reported) > 0 {
		if <-reported == "renamed" {
			found = true
		}
	}
	if !found {
		t.Fatal("renamed session was not reported")
	}
}
