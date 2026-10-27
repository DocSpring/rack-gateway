package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/terminal"
	"github.com/DocSpring/rack-gateway/internal/cli/update"
)

const (
	updateCheckFile    = "update-check.json"
	updateCheckTimeout = 5 * time.Second
	// updateNoticeWait is how long the CLI waits, after a command, for an update check that is still running.
	updateNoticeWait = time.Second
)

// skipUpdateCheckCommands never show the update notice: they're about updates and versions, or shells and scripts
// read their output.
var skipUpdateCheckCommands = map[string]bool{
	"update":                        true,
	"version":                       true,
	"build-info":                    true,
	"completion":                    true,
	"help":                          true,
	cobra.ShellCompRequestCmd:       true,
	cobra.ShellCompNoDescRequestCmd: true,
}

// pendingUpdateCheck is the update check the current command started, if any.
var pendingUpdateCheck *update.Check

// startUpdateCheck starts a background check for a newer release, at most once a day (see update.Checker).
func startUpdateCheck(cmd *cobra.Command) {
	if !updateCheckEnabled(cmd) || !terminal.StderrIsTerminal() {
		return
	}
	checker := &update.Checker{
		Client:  update.NewClient(Version, updateCheckTimeout),
		Current: Version,
		Load: func() ([]byte, error) {
			return fs.ReadFile(os.DirFS(ConfigPath), updateCheckFile)
		},
		Save: func(data []byte) error {
			if err := ensurePrivateDir(ConfigPath); err != nil {
				return err
			}
			return writePrivateFileAtomically(filepath.Join(ConfigPath, updateCheckFile), data)
		},
		Now: time.Now,
	}
	pendingUpdateCheck = checker.Start()
}

// updateCheckEnabled reports whether this command checks for updates: only release builds outside CI, unless
// RACK_GATEWAY_NO_UPDATE_CHECK is set. startUpdateCheck also needs stderr to be a terminal.
func updateCheckEnabled(cmd *cobra.Command) bool {
	if !update.IsReleaseVersion(Version) || os.Getenv("RACK_GATEWAY_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		if skipUpdateCheckCommands[c.Name()] {
			return false
		}
	}
	return true
}

// PrintUpdateNotice prints a notice to stderr when a newer release is available. main calls it after the command,
// and after its error.
func PrintUpdateNotice() {
	if pendingUpdateCheck == nil {
		return
	}
	if notice := pendingUpdateCheck.Notice(updateNoticeWait); notice != "" {
		_, _ = fmt.Fprintf(os.Stderr, "\n%s\n", displayText(notice))
	}
}
