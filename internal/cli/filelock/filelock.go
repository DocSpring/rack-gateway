// Package filelock takes exclusive locks that several rack-gateway commands share: a lock file that one process
// at a time holds. The operating system releases a lock when its process exits, so a command that crashes or is
// killed never leaves one behind.
package filelock

import (
	"os"
	"path/filepath"
)

// Lock takes the exclusive lock on the file at path, creating it (and its directory, private to the user) when it
// doesn't exist. When another process holds the lock, it calls waiting, then waits until it can take it. The
// returned func releases the lock.
func Lock(path string, waiting func()) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	locked, err := tryLockFile(file)
	if err == nil && !locked {
		if waiting != nil {
			waiting()
		}
		err = lockFile(file)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = unlockFile(file)
		_ = file.Close()
	}, nil
}
