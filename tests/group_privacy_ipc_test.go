//go:build !windows

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"rexpresence/internal/discord"
	"rexpresence/internal/presence"
)

func TestGroupPrivacyClearsDiscordActivity(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rp-privacy-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))
	ctx, cancel := context.WithCancel(context.Background())
	raw, updates := make(chan *presence.Activity, 1), make(chan *presence.Activity)
	finished := make(chan error, 2)
	go func() { finished <- presence.ApplyMode(ctx, raw, updates, nil, nil) }()
	go func() { finished <- discord.Publish(ctx, "123", "team-logo", updates, func(string) {}) }()
	defer func() {
		cancel()
		<-finished
		<-finished
	}()
	const group = "session:01a10d99-0c06-7795-bf67-798ce4c36a33"
	raw <- &presence.Activity{SessionID: group, Group: "Demo", Details: "Editing", Started: 1234}
	first := ipcAccept(t, listener)
	ipcReady(t, first)
	ipcActivity(t, first, "Editing", 1234, "Group: Demo")

	if err := presence.SetGroupPrivate(group, true); err != nil {
		t.Fatal(err)
	}
	ipcActivity(t, first, "", 0)
	if err := presence.SetGroupPrivate(group, false); err != nil {
		t.Fatal(err)
	}
	second := ipcAccept(t, listener)
	ipcReady(t, second)
	ipcActivity(t, second, "Editing", 1234, "Group: Demo")
}
