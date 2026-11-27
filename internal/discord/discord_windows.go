package discord

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialIPC(ctx context.Context) (net.Conn, error) {
	for i := 0; i < 10; i++ {
		conn, err := winio.DialPipeContext(ctx, fmt.Sprintf(`\\?\pipe\discord-ipc-%d`, i))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errDiscordUnavailable
}
