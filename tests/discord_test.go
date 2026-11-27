//go:build !windows

package integration_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rexpresence/internal/discord"
	"rexpresence/internal/presence"
)

func ipcListen(t *testing.T, path string) *net.UnixListener {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func ipcAccept(t *testing.T, listener *net.UnixListener) net.Conn {
	t.Helper()
	_ = listener.SetDeadline(time.Now().Add(4 * time.Second))
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func ipcRead(t *testing.T, conn net.Conn) (uint32, map[string]any) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	var header [8]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, binary.LittleEndian.Uint32(header[4:]))
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if err := json.Unmarshal(body, &frame); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(header[:]), frame
}

func ipcWrite(t *testing.T, conn net.Conn, opcode uint32, payload string) {
	t.Helper()
	frame := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(frame, opcode)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(payload)))
	copy(frame[8:], payload)
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func ipcReady(t *testing.T, conn net.Conn) {
	t.Helper()
	op, hello := ipcRead(t, conn)
	if op != 0 || hello["v"] != float64(1) || hello["client_id"] != "123" {
		t.Fatalf("invalid handshake: %d %v", op, hello)
	}
	ipcWrite(t, conn, 1, `{"cmd":"DISPATCH","evt":"READY"}`)
}

func ipcActivity(t *testing.T, conn net.Conn, details string, started int64, state ...string) {
	t.Helper()
	op, frame := ipcRead(t, conn)
	if op != 1 || frame["cmd"] != "SET_ACTIVITY" {
		t.Fatalf("invalid command: %d %v", op, frame)
	}
	args, ok := frame["args"].(map[string]any)
	if !ok {
		t.Fatal("missing args")
	}
	if details == "" {
		if args["activity"] != nil {
			t.Fatal("activity was not cleared")
		}
		return
	}
	activity, ok := args["activity"].(map[string]any)
	if !ok || activity["details"] != details {
		t.Fatalf("invalid activity: %v", args["activity"])
	}
	wantState := ""
	if len(state) > 0 {
		wantState = state[0]
	}
	if got, exists := activity["state"]; (wantState == "" && exists) || (wantState != "" && got != wantState) {
		t.Fatalf("state = %v (present: %v), want %q", got, exists, wantState)
	}
	assets, ok := activity["assets"].(map[string]any)
	if !ok || assets["large_image"] != "team-logo" {
		t.Fatalf("missing custom logo: %v", activity["assets"])
	}
	timestamps, ok := activity["timestamps"].(map[string]any)
	if !ok || timestamps["start"] != float64(started) {
		t.Fatalf("invalid timestamps: %v", activity["timestamps"])
	}
}

func TestDiscordReconnectClearAndCancel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	path := filepath.Join(dir, "discord-ipc-0")
	listener := ipcListen(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan *presence.Activity)
	finished := make(chan error, 1)
	go func() { finished <- discord.Publish(ctx, "123", "team-logo", updates, func(string) {}) }()
	updates <- &presence.Activity{Details: "Editing", Group: "polished-sierra", Started: 1234}
	first := ipcAccept(t, listener)
	ipcReady(t, first)
	ipcActivity(t, first, "Editing", 1234, "Group: polished-sierra")
	first.Close()
	second := ipcAccept(t, listener)
	ipcReady(t, second)
	ipcActivity(t, second, "Editing", 1234, "Group: polished-sierra")
	updates <- &presence.Activity{Details: "Editing", Started: 1234}
	ipcActivity(t, second, "Editing", 1234)
	second.Close()
	second = ipcAccept(t, listener)
	ipcReady(t, second)
	ipcActivity(t, second, "Editing", 1234)
	updates <- nil
	ipcActivity(t, second, "", 0)
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := second.Read(b[:]); err != io.EOF {
		t.Fatalf("connection not closed: %v", err)
	}
	updates <- &presence.Activity{Details: "Working", Started: 5678}
	third := ipcAccept(t, listener)
	ipcReady(t, third)
	ipcActivity(t, third, "Working", 5678)
	cancel()
	ipcActivity(t, third, "", 0)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("publisher did not stop")
	}
}

func TestCardChangesReachDiscordWithoutRexEvent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))
	set := func(field, value string) {
		t.Helper()
		if err := presence.SetCardField(field, value); err != nil {
			t.Fatal(err)
		}
	}
	set("line1", "{tool}: {project}")
	set("line2", "In {group}")
	set("logo", "")
	set("timer", "off")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := make(chan *presence.Activity, 1)
	raw <- &presence.Activity{Details: "Vim", Project: "Docs", Group: "{tool}", Started: 1234}
	out := make(chan *presence.Activity)
	done := make(chan error, 2)
	go func() { done <- presence.ApplyMode(ctx, raw, out, nil, nil) }()
	go func() { done <- discord.Publish(ctx, "123", "startup-logo", out, func(string) {}) }()
	conn := ipcAccept(t, listener)
	ipcReady(t, conn)
	check := func(details, state string) {
		t.Helper()
		_, frame := ipcRead(t, conn)
		card := frame["args"].(map[string]any)["activity"].(map[string]any)
		for field, want := range map[string]string{"details": details, "state": state, "assets": "", "timestamps": ""} {
			got, exists := card[field]
			if exists != (want != "") || exists && got != want {
				t.Fatalf("%s = %v, want %q (empty means omitted)", field, got, want)
			}
		}
	}
	check("Vim: Docs", "In {tool}") // Do not expand placeholder-looking group names twice.
	set("line2", "")
	check("Vim: Docs", "")
	set("line1", "")
	check("", "")
	if err := presence.SetMode("custom", "{group} is literal"); err != nil {
		t.Fatal(err)
	}
	check("{group} is literal", "")
	if err := presence.SetMode("paused", ""); err != nil {
		t.Fatal(err)
	}
	ipcActivity(t, conn, "", 0)
	cancel()
	for range 2 {
		if err := <-done; err != nil && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestDiscordGroupSanitization(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan *presence.Activity)
	finished := make(chan error, 1)
	go func() { finished <- discord.Publish(ctx, "123", "team-logo", updates, func(string) {}) }()
	updates <- &presence.Activity{Details: "Editing", Group: " \tpolished\n\x00sierra\xff ", Started: 1}
	conn := ipcAccept(t, listener)
	ipcReady(t, conn)
	ipcActivity(t, conn, "Editing", 1, "Group: polished sierra")
	updates <- &presence.Activity{Details: "Editing", Group: " \t\n\x00\xff", Started: 1}
	ipcActivity(t, conn, "Editing", 1)
	updates <- &presence.Activity{Details: "Editing", Group: strings.Repeat("é", 122), Started: 1}
	ipcActivity(t, conn, "Editing", 1, "Group: "+strings.Repeat("é", 121))
	cancel()
	ipcActivity(t, conn, "", 0)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestDiscordRPCErrorStopsWithoutPrivatePayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan *presence.Activity)
	finished := make(chan error, 1)
	go func() { finished <- discord.Publish(ctx, "123", "team-logo", updates, func(string) {}) }()
	updates <- &presence.Activity{Details: "private", Started: 1}
	conn := ipcAccept(t, listener)
	ipcRead(t, conn)
	ipcWrite(t, conn, 1, `{"evt":"ERROR","data":{"code":4000,"message":"private"}}`)
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "4000") || strings.Contains(err.Error(), "private") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("publisher retried RPC error")
	}
}

func TestDiscordReportsOnlyLatestAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan *presence.Activity)
	statuses := make(chan string, 32)
	finished := make(chan error, 1)
	go func() {
		finished <- discord.Publish(ctx, "123", "team-logo", updates, func(status string) { statuses <- status })
	}()
	wantStatus := func(want string) {
		t.Helper()
		select {
		case got := <-statuses:
			if got != want {
				t.Fatalf("status = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing status %q", want)
		}
	}
	wantStatus("Waiting for active presence")
	updates <- &presence.Activity{Details: "private", Started: 1}
	wantStatus("Connecting to Discord")
	conn := ipcAccept(t, listener)
	ipcReady(t, conn)
	_, first := ipcRead(t, conn)
	wantStatus("Awaiting Discord acknowledgement")
	updates <- &presence.Activity{Details: "new private", Started: 2}
	_, second := ipcRead(t, conn)
	ipcWrite(t, conn, 1, `{"cmd":"SET_ACTIVITY","nonce":"`+first["nonce"].(string)+`"}`)
	ipcWrite(t, conn, 1, `{"cmd":"OTHER","nonce":"`+second["nonce"].(string)+`"}`)
	ipcWrite(t, conn, 1, `{"cmd":"SET_ACTIVITY","evt":"OTHER","nonce":"`+second["nonce"].(string)+`"}`)
	ipcWrite(t, conn, 3, `{}`)
	if op, _ := ipcRead(t, conn); op != 4 {
		t.Fatalf("expected pong, got %d", op)
	}
	select {
	case status := <-statuses:
		t.Fatalf("stale acknowledgement reported %q", status)
	default:
	}
	ipcWrite(t, conn, 1, `{"cmd":"SET_ACTIVITY","nonce":"`+second["nonce"].(string)+`"}`)
	wantStatus("Discord accepted activity")
	conn.Close()
	wantStatus("Discord disconnected")
	secondConn := ipcAccept(t, listener)
	wantStatus("Connecting to Discord")
	ipcReady(t, secondConn)
	_, third := ipcRead(t, secondConn)
	wantStatus("Awaiting Discord acknowledgement")
	ipcWrite(t, secondConn, 1, `{"cmd":"SET_ACTIVITY","nonce":"`+second["nonce"].(string)+`"}`)
	ipcWrite(t, secondConn, 3, `{}`)
	if op, _ := ipcRead(t, secondConn); op != 4 {
		t.Fatalf("expected pong, got %d", op)
	}
	select {
	case status := <-statuses:
		t.Fatalf("previous connection acknowledgement reported %q", status)
	default:
	}
	ipcWrite(t, secondConn, 1, `{"cmd":"SET_ACTIVITY","nonce":"`+third["nonce"].(string)+`"}`)
	wantStatus("Discord accepted activity")
	updates <- nil
	ipcActivity(t, secondConn, "", 0)
	wantStatus("Discord disconnected")
	wantStatus("No active presence")
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("publisher did not stop")
	}
}
