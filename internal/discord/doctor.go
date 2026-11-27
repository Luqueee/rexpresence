package discord

import (
	"context"
)

// CheckIPC checks whether a Discord IPC endpoint can be opened without sending data.
func CheckIPC(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()

	conn, err := dialIPC(ctx)
	if err != nil {
		return err
	}
	return conn.Close()
}
