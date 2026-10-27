package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	binaryName         = "rack-gateway"
	maxArchiveBytes    = 256 << 20
	maxBinaryBytes     = 512 << 20
	maxSmallAssetBytes = 1 << 20
	buildInfoTimeout   = time.Minute

	securityKeysSupported   = "supported"
	securityKeysUnsupported = "not supported"
)

// BuildInfo is what `rack-gateway build-info` reports about a CLI binary.
type BuildInfo struct {
	Version      string
	SecurityKeys bool
}

// String formats the build info as `rack-gateway build-info` prints it.
func (b BuildInfo) String() string {
	keys := securityKeysUnsupported
	if b.SecurityKeys {
		keys = securityKeysSupported
	}
	return fmt.Sprintf("version: %s\nsecurity keys: %s\n", b.Version, keys)
}

// ParseBuildInfo parses the output of `rack-gateway build-info`.
func ParseBuildInfo(out []byte) (BuildInfo, error) {
	var info BuildInfo
	var keys string
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(line, "version: "); ok {
			info.Version = value
		} else if value, ok := strings.CutPrefix(line, "security keys: "); ok {
			keys = value
		}
	}
	if info.Version == "" || (keys != securityKeysSupported && keys != securityKeysUnsupported) {
		return BuildInfo{}, fmt.Errorf("unexpected build-info output: %q", out)
	}
	info.SecurityKeys = keys == securityKeysSupported
	return info, nil
}

// Installer replaces the running CLI binary with a release.
type Installer struct {
	Client *Client
	// Verify checks a release archive's provenance attestation bundle (see ProvenanceVerifier.Verify).
	Verify func(bundleJSON, digest []byte, tag string) error
	// BuildInfo runs `build-info` on a downloaded binary (see RunBuildInfo).
	BuildInfo  func(ctx context.Context, path string) ([]byte, error)
	Executable string
	Current    BuildInfo
	GOOS       string
	GOARCH     string
	Out        io.Writer
}

// Install downloads the release's archive for this platform, verifies its checksum and build provenance, checks
// that the binary inside runs and is a release this build can be replaced with, and then renames it over the
// running binary. Until that rename, the installed binary is untouched.
func (i *Installer) Install(ctx context.Context, rel *Release) error {
	asset, ok := AssetName(i.GOOS, i.GOARCH)
	if !ok {
		return fmt.Errorf("releases have no build for %s/%s: build the CLI from source", i.GOOS, i.GOARCH)
	}
	dir := filepath.Dir(i.Executable)
	tmp, err := os.CreateTemp(dir, "."+binaryName+".update-*")
	if err != nil {
		return fmt.Errorf(
			"can't write to %s (%w). Run \"sudo rack-gateway update\", or reinstall the CLI into a directory you own",
			dir, err)
	}
	installed := false
	defer func() {
		if !installed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	archive, err := i.download(ctx, rel.Tag, asset)
	if err != nil {
		return err
	}
	if err := extractBinary(archive, tmp); err != nil {
		return err
	}
	if err := i.check(ctx, tmp.Name(), rel); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), i.Executable); err != nil {
		return err
	}
	installed = true
	return nil
}

// download returns the release archive once its checksum and its build provenance attestation check out.
func (i *Installer) download(ctx context.Context, tag, asset string) ([]byte, error) {
	_, _ = fmt.Fprintf(i.Out, "Downloading %s from %s\n", asset, i.Client.ReleaseURL(tag))
	archive, err := i.Client.Download(ctx, tag, asset, maxArchiveBytes)
	if err != nil {
		return nil, err
	}
	checksum, err := i.Client.Download(ctx, tag, asset+".sha256", maxSmallAssetBytes)
	if err != nil {
		return nil, err
	}
	digest, err := VerifyChecksum(archive, checksum, asset)
	if err != nil {
		return nil, err
	}
	bundleJSON, err := i.Client.Download(ctx, tag, asset+".sigstore.json", maxSmallAssetBytes)
	if err != nil {
		return nil, err
	}
	if err := i.Verify(bundleJSON, digest, tag); err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(i.Out, "Verified its checksum and build provenance (signed by %s)\n",
		SignerIdentity(i.Client.Repository, tag))
	return archive, nil
}

// check makes the new binary executable like the one it replaces, runs it, and checks it is the release we asked
// for and keeps security key support when this build has it.
func (i *Installer) check(ctx context.Context, path string, rel *Release) error {
	current, err := os.Stat(i.Executable)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, current.Mode().Perm()); err != nil {
		return err
	}
	out, err := i.BuildInfo(ctx, path)
	if err != nil {
		return fmt.Errorf("the downloaded binary doesn't run on this machine: %w", err)
	}
	next, err := ParseBuildInfo(out)
	if err != nil {
		return err
	}
	if TagFor(next.Version) != rel.Tag {
		return fmt.Errorf("the downloaded binary reports version %s, not %s", next.Version, rel.Version())
	}
	if i.Current.SecurityKeys && !next.SecurityKeys {
		return fmt.Errorf("the %s/%s release has no security key support, and this build has it: "+
			"build the CLI from source to keep it", i.GOOS, i.GOARCH)
	}
	return nil
}

// extractBinary writes the archive's rack-gateway binary to dst and closes it.
func extractBinary(archive []byte, dst *os.File) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("invalid release archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the release archive has no %s binary", binaryName)
		}
		if err != nil {
			return fmt.Errorf("invalid release archive: %w", err)
		}
		if hdr.Name != binaryName {
			continue
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size <= 0 || hdr.Size > maxBinaryBytes {
			return fmt.Errorf("the release archive's %s is not a regular file of a sensible size", binaryName)
		}
		if _, err := io.CopyN(dst, tr, hdr.Size); err != nil {
			return fmt.Errorf("invalid release archive: %w", err)
		}
		if err := dst.Sync(); err != nil {
			return err
		}
		return dst.Close()
	}
}

// RunBuildInfo runs `build-info` on the binary at path and returns its output.
func RunBuildInfo(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, buildInfoTimeout)
	defer cancel()
	// path is the binary Install just extracted from a release archive whose provenance it verified.
	cmd := exec.CommandContext(ctx, path, "build-info")
	cmd.Env = append(os.Environ(), "RACK_GATEWAY_NO_UPDATE_CHECK=1")
	return cmd.Output()
}
