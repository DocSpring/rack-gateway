package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/update"
	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

const updateTimeout = 10 * time.Minute

// UpdateCommand returns the update command, which replaces this binary with a verified release from GitHub.
func UpdateCommand() *cobra.Command {
	var checkOnly bool
	var version string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the rack-gateway CLI to the latest release",
		Long: `Update the rack-gateway CLI to the latest release on GitHub.

The update checks the release archive's SHA-256 checksum and its build provenance attestation, which must be
signed by the repository's Release workflow for that release's tag, before it replaces this binary.

Releases include builds for macOS on Apple Silicon and for Linux (x86_64 and arm64). The Linux builds have no
security key support, so a Linux build with it isn't replaced: build that one from source.`,
		Args: cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), updateTimeout)
			defer cancel()
			return runUpdate(ctx, cmd, update.NewClient(Version, time.Minute), checkOnly, version)
		}),
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check whether a newer release is available")
	cmd.Flags().StringVar(&version, "version", "",
		"Install this release (for example v0.1.29), even when it's older than this one")
	return cmd
}

func runUpdate(ctx context.Context, cmd *cobra.Command, client *update.Client, checkOnly bool, version string) error {
	if !update.IsReleaseVersion(Version) {
		return fmt.Errorf("this is a development build (%s): install a release with the install script instead",
			Version)
	}
	rel, err := targetRelease(ctx, client, version)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	replaceable := update.Replaceable(runtime.GOOS, runtime.GOARCH, webauthn.Supported)
	switch {
	case rel.Version() == Version && version != "":
		_, _ = fmt.Fprintf(out, "rack-gateway %s is already installed.\n", Version)
		return nil
	case rel.Version() == Version:
		_, _ = fmt.Fprintf(out, "rack-gateway %s is the latest release.\n", Version)
		return nil
	case version == "" && update.IsNewer(Version, rel.Version()):
		_, _ = fmt.Fprintf(out, "rack-gateway %s is newer than the latest release (%s).\n", Version, rel.Version())
		return nil
	case checkOnly:
		printAvailableRelease(out, client, rel, replaceable)
		return nil
	case replaceable != nil:
		return replaceable
	}
	return installRelease(ctx, cmd, client, rel)
}

// printAvailableRelease says a release is available, and how to install it: `rack-gateway update`, unless no
// release can replace this build (replaceable).
func printAvailableRelease(out io.Writer, client *update.Client, rel *update.Release, replaceable error) {
	_, _ = fmt.Fprintf(out, "rack-gateway %s is available (this is %s): %s\n",
		rel.Version(), Version, client.ReleaseURL(rel.Tag))
	if replaceable != nil {
		_, _ = fmt.Fprintf(out, "rack-gateway update can't install it: %s.\n", replaceable)
		return
	}
	_, _ = fmt.Fprintln(out, `Run "rack-gateway update" to install it.`)
}

// targetRelease returns the release named by --version, or the latest release.
func targetRelease(ctx context.Context, client *update.Client, version string) (*update.Release, error) {
	if version == "" {
		rel, err := client.LatestRelease(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to find the latest release: %w", err)
		}
		return rel, nil
	}
	if !update.IsReleaseVersion(version) {
		return nil, fmt.Errorf("invalid --version %q (expected a release such as v0.1.29)", version)
	}
	tag := update.TagFor(version)
	rel, err := client.ReleaseByTag(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to find release %s: %w", tag, err)
	}
	return rel, nil
}

func installRelease(ctx context.Context, cmd *cobra.Command, client *update.Client, rel *update.Release) error {
	executable, err := currentExecutable()
	if err != nil {
		return err
	}
	verifier, err := update.NewProvenanceVerifier(client.Repository)
	if err != nil {
		return err
	}
	installer := &update.Installer{
		Client:     client,
		Verify:     verifier.Verify,
		BuildInfo:  update.RunBuildInfo,
		Executable: executable,
		Current:    currentBuildInfo(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		Out:        cmd.OutOrStdout(),
	}
	if err := installer.Install(ctx, rel); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s from %s to %s.\n", executable, Version, rel.Version())
	return nil
}

// currentExecutable returns the path of the running binary, with symlinks resolved.
func currentExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("can't find the running rack-gateway binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("can't find the running rack-gateway binary: %w", err)
	}
	return resolved, nil
}

func currentBuildInfo() update.BuildInfo {
	return update.BuildInfo{Version: Version, SecurityKeys: webauthn.Supported}
}

// BuildInfoCommand returns the hidden build-info command. `rack-gateway update` runs it on a downloaded binary to
// check that it runs on this machine, is the release it asked for and keeps security key support.
func BuildInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "build-info",
		Short:  "Show the CLI version and whether it supports security keys",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), currentBuildInfo().String())
			return err
		},
	}
}
