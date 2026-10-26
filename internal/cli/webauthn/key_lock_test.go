//go:build !nofido

package webauthn

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/keys-pub/go-libfido2"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cli/filelock"
)

// Each way a command uses the security key holds the lock, so a command waits for another that's using the key
// rather than both failing. (No key is opened: the test replaces opening one.)
func TestSecurityKeyUsersWaitForTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-key.lock")
	lockPath, open := keyLockPath, openDevices
	t.Cleanup(func() { keyLockPath, openDevices = lockPath, open })
	keyLockPath = func() (string, error) { return path, nil }
	errNoKey := errors.New("no security key in tests")
	openDevices = func() ([]*libfido2.Device, error) { return nil, errNoKey }

	for name, use := range map[string]func() error{
		"assertion": func() error {
			_, _, err := GetAssertionWithCachedPIN(AssertionOptions{}, "1234")
			return err
		},
		"PIN check": func() error { return CheckPIN("1234") },
		"PIN query": func() error {
			if SingleKeyHasPIN() {
				return errors.New("SingleKeyHasPIN found a PIN without a key")
			}
			return errNoKey
		},
	} {
		unlock, err := filelock.Lock(path, nil)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- use() }()
		select {
		case <-done:
			t.Fatalf("the %s used the key while another command held the lock", name)
		case <-time.After(200 * time.Millisecond):
		}
		unlock()
		require.ErrorIs(t, <-done, errNoKey, name)
	}
}
