//go:build !nofido

package webauthn

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DocSpring/rack-gateway/internal/cli/filelock"
)

// keyLockPath is the lock every rack-gateway command holds while it uses a security key. It's in the user's cache
// directory, which every command shares (each agent session can have a TMPDIR of its own).
var keyLockPath = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "rack-gateway", "security-key.lock")
}

// withKeyLock runs use while holding the security key lock, waiting for any other rack-gateway command that holds
// it. A key answers one command at a time: two commands asking for a PIN and a touch at once (two agents running
// commands, say) both fail, with libfido2's "rx error" or "unsupported option", and the person can't tell which
// touch is for which. A command holds the lock from its PIN dialog to its touch, so the next one asks only then.
func withKeyLock[T any](use func() (T, error)) (T, error) {
	unlock, err := filelock.Lock(keyLockPath(), func() {
		fmt.Fprintln(os.Stderr, "Waiting for another rack-gateway command to finish with the security key...")
	})
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to lock the security key for this command: %w", err)
	}
	defer unlock()
	return use()
}
