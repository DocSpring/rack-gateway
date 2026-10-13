package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

// pause waits for d between polls, returning early with the error when the command's context ends.
func pause(cmd *cobra.Command, d time.Duration) error {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
