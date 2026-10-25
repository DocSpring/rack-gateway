// Package terminal tells whether the CLI's standard streams are terminals.
package terminal

import (
	"os"

	"golang.org/x/term"
)

// StdinFD returns stdin's file descriptor when stdin is a terminal.
func StdinFD() (int, bool) {
	return terminalFD(os.Stdin)
}

// IsInteractive reports whether stdin and stdout are both terminals.
func IsInteractive() bool {
	_, in := terminalFD(os.Stdin)
	_, out := terminalFD(os.Stdout)
	return in && out
}

func terminalFD(file *os.File) (int, bool) {
	fd := file.Fd()
	const maxInt = int(^uint(0) >> 1)
	if fd > uintptr(maxInt) || !term.IsTerminal(int(fd)) {
		return 0, false
	}
	return int(fd), true
}
