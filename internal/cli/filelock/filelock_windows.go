//go:build windows

package filelock

import (
	"errors"
	"math"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(file *os.File) error {
	return lockFileEx(file, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

// tryLockFile takes the lock if no other process holds it, and reports whether it did.
func tryLockFile(file *os.File) (bool, error) {
	err := lockFileEx(file, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func lockFileEx(file *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, math.MaxUint32, math.MaxUint32,
		&windows.Overlapped{})
}

func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, math.MaxUint32, math.MaxUint32, &windows.Overlapped{})
}
