package dbtest

import (
	"context"
	"time"
)

// retry calls attempt every interval until it succeeds, returns an error it can't retry, or timeout passes. It
// returns attempt's last error (nil on success).
func retry(timeout, interval time.Duration, attempt func() (retryable bool, err error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		retryable, err := attempt()
		if err == nil || !retryable {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
		}
	}
}
