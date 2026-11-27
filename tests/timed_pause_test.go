package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rexpresence/internal/presence"
)

func TestModeTimedPause(t *testing.T) {
	for _, tc := range []struct{ original, override, want string }{
		{"auto", "", "active tool"},
		{"custom", "", "my text"},
		{"custom", "auto", "active tool"},
	} {
		t.Run(tc.original+"/override="+tc.override, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			set := func(mode, text string) {
				t.Helper()
				if err := presence.SetMode(mode, text); err != nil {
					t.Fatal(err)
				}
			}
			set(tc.original, "my text")
			start := time.Now()
			set("pause-15m", "")
			set("pause-15m", "") // Extending a pause must not lose its original mode.
			path := filepath.Join(home, ".rexpresence", "mode")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved map[string]any
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			deadline, ok := saved["until"].(float64)
			if !ok || int64(deadline) < start.Add(15*time.Minute).UnixMilli() || int64(deadline) > time.Now().Add(15*time.Minute).UnixMilli() {
				t.Fatalf("deadline is not 15 minutes from selection: %v", deadline)
			}
			ctx, cancel := context.WithCancel(context.Background())
			input := make(chan *presence.Activity)
			out := make(chan *presence.Activity)
			done := make(chan error, 1)
			go func() { done <- presence.ApplyMode(ctx, input, out, nil, nil) }()
			defer func() { cancel(); <-done }()
			input <- &presence.Activity{Details: "active tool", Group: "group"}
			receive := func() *presence.Activity {
				t.Helper()
				select {
				case p := <-out:
					return p
				case <-time.After(3 * time.Second):
					t.Fatal("no activity update")
					return nil
				}
			}
			if p := receive(); p != nil {
				t.Fatalf("restart during pause published %#v", p)
			}
			// Shorten only the deadline in the fixture; let the real watcher handle expiration.
			saved["until"] = time.Now().UnixMilli() + 100
			data, err = json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.override != "" {
				set(tc.override, "")
			}
			p := receive()
			if p == nil || p.Details != tc.want || p.Custom && p.Group != "" {
				t.Fatalf("wrong activity after pause: %#v", p)
			}
		})
	}
}

func TestModeResumeNow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	set := func(mode, text string) {
		t.Helper()
		if err := presence.SetMode(mode, text); err != nil {
			t.Fatal(err)
		}
	}
	left := func(want int64) {
		t.Helper()
		if got, err := presence.PauseMinutes(); err != nil || got != want {
			t.Fatalf("remaining = %d, %v; want %d", got, err, want)
		}
	}
	path := filepath.Join(home, ".rexpresence", "mode")
	for _, mode := range []string{"auto", "custom"} {
		set(mode, "my saved text")
		left(0)
		set("pause-15m", "")
		left(15)
		if err := presence.Resume(); err != nil {
			t.Fatal(err)
		}
		left(0)
		data, err := os.ReadFile(path)
		if mode == "auto" {
			if !os.IsNotExist(err) {
				t.Fatalf("automatic mode not restored: %v", err)
			}
		} else {
			var restored struct{ Mode, Text string }
			if err != nil || json.Unmarshal(data, &restored) != nil || restored.Mode != "custom" || restored.Text != "my saved text" {
				t.Fatal("custom presence not restored")
			}
		}
	}
	set("pause-15m", "")
	set("paused", "")
	left(0)
	if err := presence.Resume(); err == nil {
		t.Fatal("stale resume accepted after indefinite pause")
	}
	for _, tc := range []struct {
		delta time.Duration
		want  int64
	}{{7*time.Minute + 30*time.Second, 8}, {20 * time.Second, 1}, {-time.Second, 0}} {
		data, err := json.Marshal(map[string]any{"mode": "pause-15m", "resume": "auto", "until": time.Now().Add(tc.delta).UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		left(tc.want)
	}
	if err := presence.Resume(); err == nil {
		t.Fatal("expired pause resumed")
	}
}
