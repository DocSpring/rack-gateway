//go:build !nofido

package webauthn

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DocSpring/rack-gateway/internal/cli/filelock"
)

// keyLockPath is the lock every rack-gateway command holds while it uses a security key. It's in the user's cache
// directory, which every command shares. A temporary directory isn't: each agent session can have a TMPDIR of its
// own, so commands there would use the key at once.
var keyLockPath = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rack-gateway", "security-key.lock"), nil
}

// withKeyLock runs use while holding the security key lock, waiting for any other rack-gateway command that holds
// it. A key answers one command at a time: two commands asking for a PIN and a touch at once (two agents running
// commands, say) both fail, with libfido2's "rx error" or "unsupported option", and the person can't tell which
// touch is for which. A command holds the lock from its PIN dialog to its touch, so the next one asks only then.
func withKeyLock[T any](use func() (T, error)) (T, error) {
	var zero T
	path, err := keyLockPath()
	if err != nil {
		return zero, fmt.Errorf("failed to find where to lock the security key: %w", err)
	}
	unlock, err := filelock.Lock(path, func() {
		fmt.Fprintln(os.Stderr, "Waiting for another rack-gateway command to finish with the security key...")
	})
	if err != nil {
		return zero, fmt.Errorf("failed to lock the security key for this command: %w", err)
	}
	defer unlock()
	return use()
}
