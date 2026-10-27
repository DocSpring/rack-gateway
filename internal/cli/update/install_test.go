package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testAsset = "rack-gateway-linux-amd64.tar.gz"

type tarEntry struct {
	name     string
	body     string
	typeflag byte
}

func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: e.typeflag}
		if e.typeflag == tar.TypeSymlink {
			hdr.Size = 0
			hdr.Linkname = "/etc/passwd"
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if hdr.Size > 0 {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

type releaseFixture struct {
	archive []byte
	// checksum is the .sha256 file; nil means the archive's correct one.
	checksum []byte
	verified [][]byte
	verify   error
	info     BuildInfo
	exe      string
	goos     string
	goarch   string
}

func newReleaseFixture(t *testing.T) *releaseFixture {
	t.Helper()
	archive := tarGz(t, tarEntry{name: "rack-gateway", body: "new binary", typeflag: tar.TypeReg})
	exe := filepath.Join(t.TempDir(), "rack-gateway")
	require.NoError(t, os.WriteFile(exe, []byte("old binary"), 0o600))
	binaryMode := os.FileMode(0o751)
	require.NoError(t, os.Chmod(exe, binaryMode))
	return &releaseFixture{
		archive: archive,
		info:    BuildInfo{Version: "0.1.29", SecurityKeys: true},
		exe:     exe,
		goos:    "linux",
		goarch:  "amd64",
	}
}

func (f *releaseFixture) installer(t *testing.T, current BuildInfo) *Installer {
	t.Helper()
	asset, _ := AssetName(f.goos, f.goarch)
	checksum := f.checksum
	if checksum == nil {
		sum := sha256.Sum256(f.archive)
		checksum = []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n")
	}
	assets := map[string][]byte{
		asset:                    f.archive,
		asset + ".sha256":        checksum,
		asset + ".sigstore.json": []byte(`{"bundle":true}`),
	}
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := assets[filepath.Base(r.URL.Path)]
		if !ok || filepath.Dir(r.URL.Path) != "/DocSpring/rack-gateway/releases/download/v0.1.29" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	return &Installer{
		Client: client,
		Verify: func(bundleJSON, digest []byte, tag string) error {
			require.Equal(t, `{"bundle":true}`, string(bundleJSON))
			require.Equal(t, "v0.1.29", tag)
			f.verified = append(f.verified, digest)
			return f.verify
		},
		BuildInfo: func(_ context.Context, path string) ([]byte, error) {
			body, err := os.ReadFile(filepath.Clean(path))
			require.NoError(t, err)
			require.Equal(t, "new binary", string(body))
			return []byte(f.info.String()), nil
		},
		Executable: f.exe,
		Current:    current,
		GOOS:       f.goos,
		GOARCH:     f.goarch,
		Out:        &bytes.Buffer{},
	}
}

func (f *releaseFixture) requireUnchanged(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile(f.exe)
	require.NoError(t, err)
	require.Equal(t, "old binary", string(body))
	entries, err := os.ReadDir(filepath.Dir(f.exe))
	require.NoError(t, err)
	require.Len(t, entries, 1, "the temporary file is removed")
}

var release0129 = &Release{Tag: "v0.1.29"}

func TestInstallReplacesTheBinary(t *testing.T) {
	f := newReleaseFixture(t)
	f.info.SecurityKeys = false
	installer := f.installer(t, BuildInfo{Version: "0.1.28"})
	var out bytes.Buffer
	installer.Out = &out

	require.NoError(t, installer.Install(context.Background(), release0129))

	body, err := os.ReadFile(f.exe)
	require.NoError(t, err)
	require.Equal(t, "new binary", string(body))
	info, err := os.Stat(f.exe)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o751), info.Mode().Perm(), "keeps the old binary's permissions")
	sum := sha256.Sum256(f.archive)
	require.Equal(t, [][]byte{sum[:]}, f.verified)
	entries, err := os.ReadDir(filepath.Dir(f.exe))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, out.String(),
		"signed by https://github.com/DocSpring/rack-gateway/.github/workflows/release.yml@refs/tags/v0.1.29")
}

func TestInstallLeavesTheBinaryAloneWhenAStepFails(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(f *releaseFixture)
		current BuildInfo
		wantErr string
	}{
		{
			name:    "checksum mismatch",
			modify:  func(f *releaseFixture) { f.checksum = []byte(strings.Repeat("0", 64) + "  " + testAsset + "\n") },
			wantErr: "checksum mismatch for " + testAsset,
		},
		{
			name:    "attestation rejected",
			modify:  func(f *releaseFixture) { f.verify = errors.New("attestation verification failed: wrong signer") },
			wantErr: "wrong signer",
		},
		{
			name:    "binary reports another version",
			modify:  func(f *releaseFixture) { f.info.Version = "0.1.30" },
			wantErr: "reports version 0.1.30, not 0.1.29",
		},
		{
			// Only the macOS release should have security keys: this one is broken.
			name: "release would drop security key support",
			modify: func(f *releaseFixture) {
				f.goos, f.goarch = "darwin", "arm64"
				f.info.SecurityKeys = false
			},
			current: BuildInfo{Version: "0.1.28", SecurityKeys: true},
			wantErr: "the darwin/arm64 release has no security key support, and this build has it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReleaseFixture(t)
			tt.modify(f)
			err := f.installer(t, tt.current).Install(context.Background(), release0129)
			require.ErrorContains(t, err, tt.wantErr)
			f.requireUnchanged(t)
		})
	}
}

func TestInstallOnMacKeepsSecurityKeys(t *testing.T) {
	f := newReleaseFixture(t)
	f.goos, f.goarch = "darwin", "arm64"
	installer := f.installer(t, BuildInfo{Version: "0.1.28", SecurityKeys: true})
	require.NoError(t, installer.Install(context.Background(), release0129))
}

// A Linux build with security keys (built from source) is refused before anything is downloaded: the Linux
// releases have none.
func TestInstallRefusesToReplaceALinuxBuildWithSecurityKeys(t *testing.T) {
	f := newReleaseFixture(t)
	err := f.installer(t, BuildInfo{Version: "0.1.28", SecurityKeys: true}).Install(context.Background(), release0129)
	require.ErrorContains(t, err, "the linux/amd64 release has no security key support, and this build has it")
	require.Empty(t, f.verified)
	f.requireUnchanged(t)
}

func TestInstallNeedsAWritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	f := newReleaseFixture(t)
	dir := filepath.Dir(f.exe)
	readOnly, writable := os.FileMode(0o555), os.FileMode(0o755)
	require.NoError(t, os.Chmod(dir, readOnly))
	t.Cleanup(func() { _ = os.Chmod(dir, writable) })

	err := f.installer(t, BuildInfo{Version: "0.1.28"}).Install(context.Background(), release0129)
	require.ErrorContains(t, err, `Run "sudo rack-gateway update"`)
	require.Empty(t, f.verified, "nothing is downloaded")
}

func TestInstallRefusesUnsupportedPlatforms(t *testing.T) {
	f := newReleaseFixture(t)
	installer := f.installer(t, BuildInfo{Version: "0.1.28"})
	installer.GOOS, installer.GOARCH = "darwin", "amd64"
	require.ErrorContains(t, installer.Install(context.Background(), release0129),
		"releases have no build for darwin/amd64")
	f.requireUnchanged(t)
}

func TestExtractBinaryOnlyTakesTheRegularRackGatewayFile(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		wantErr string
	}{
		{
			name:    "no binary",
			archive: tarGz(t, tarEntry{name: "README.md", body: "hi", typeflag: tar.TypeReg}),
			wantErr: "has no rack-gateway binary",
		},
		{
			name:    "binary in a subdirectory",
			archive: tarGz(t, tarEntry{name: "bin/rack-gateway", body: "x", typeflag: tar.TypeReg}),
			wantErr: "has no rack-gateway binary",
		},
		{
			name:    "symlink",
			archive: tarGz(t, tarEntry{name: "rack-gateway", typeflag: tar.TypeSymlink}),
			wantErr: "not a regular file",
		},
		{
			name:    "not gzip",
			archive: []byte("plain text"),
			wantErr: "invalid release archive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst, err := os.CreateTemp(t.TempDir(), "binary")
			require.NoError(t, err)
			defer func() { _ = dst.Close() }()
			require.ErrorContains(t, extractBinary(tt.archive, dst), tt.wantErr)
		})
	}

	dst, err := os.CreateTemp(t.TempDir(), "binary")
	require.NoError(t, err)
	archive := tarGz(t,
		tarEntry{name: "LICENSE", body: "MIT", typeflag: tar.TypeReg},
		tarEntry{name: "rack-gateway", body: "binary", typeflag: tar.TypeReg})
	require.NoError(t, extractBinary(archive, dst))
	body, err := os.ReadFile(dst.Name())
	require.NoError(t, err)
	require.Equal(t, "binary", string(body))
}

func TestBuildInfoRoundTrip(t *testing.T) {
	for _, info := range []BuildInfo{{Version: "0.1.29", SecurityKeys: true}, {Version: "0.1.29"}} {
		parsed, err := ParseBuildInfo([]byte(info.String()))
		require.NoError(t, err)
		require.Equal(t, info, parsed)
	}
	_, err := ParseBuildInfo([]byte("version: 0.1.29\n"))
	require.ErrorContains(t, err, "unexpected build-info output")
	_, err = ParseBuildInfo([]byte("Error: unknown command \"build-info\"\n"))
	require.ErrorContains(t, err, "unexpected build-info output")
}
