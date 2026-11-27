//go:build !windows

package discord

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func dialIPC(ctx context.Context) (net.Conn, error) {
	dirs := []string{os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("TMPDIR"), os.Getenv("TMP"), os.Getenv("TEMP"), "/tmp"}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for i := 0; i < 10; i++ {
			conn, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i)))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
	}
	return nil, errDiscordUnavailable
}
