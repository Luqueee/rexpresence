//go:build !windows

package integration_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rexpresence/internal/discord"
)

func TestDiscordCheckIPCDoesNotSendData(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	listener := ipcListen(t, filepath.Join(dir, "discord-ipc-0"))

	if err := discord.CheckIPC(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := ipcAccept(t, listener)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if n, err := conn.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read = %d, %v; want EOF without data", n, err)
	}
}

func TestDiscordCheckIPCCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := discord.CheckIPC(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckIPC() = %v; want context canceled", err)
	}
}
