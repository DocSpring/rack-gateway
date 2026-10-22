//go:build windows

package cli

import (
	"math"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0,
		math.MaxUint32, math.MaxUint32, &windows.Overlapped{})
}

func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, math.MaxUint32, math.MaxUint32, &windows.Overlapped{})
}
