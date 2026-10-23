package proxy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const webManifest = "services:\n  web:\n    image: docker.io/org/app:abc\n"

// tarEntry is one entry of a test tarball. typeflag defaults to a regular file.
type tarEntry struct {
	name     string
	typeflag byte
	linkname string
	body     []byte
	pax      map[string]string
}

// writeTarball writes entries to w as a gzipped tarball.
func writeTarball(w io.Writer, entries ...tarEntry) error {
	return writeGzippedTar(w, true, entries...)
}

// writeGzippedTar writes entries to w as one gzip member. Without endArchive it leaves out the tar end-of-archive
// blocks, so another member can carry on the same archive.
func writeGzippedTar(w io.Writer, endArchive bool, entries ...tarEntry) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name, Typeflag: entry.typeflag, Linkname: entry.linkname, Mode: 0o644, PAXRecords: entry.pax,
		}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(entry.body))
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(entry.body); err != nil {
			return err
		}
	}
	finish := tw.Flush
	if endArchive {
		finish = tw.Close
	}
	if err := finish(); err != nil {
		return err
	}
	return gz.Close()
}

// flowSequenceManifest returns a valid manifest of size bytes, padded with a flow sequence of one-character scalars:
// the most YAML nodes per byte.
func flowSequenceManifest(size int) []byte {
	manifest := []byte(webManifest + "x: [1")
	for len(manifest) < size-1 {
		manifest = append(manifest, ",1"...)
	}
	return append(manifest, ']')
}

func gzippedTarball(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, writeTarball(&buf, entries...))
	return buf.Bytes()
}

// streamedTarball serves entries as a gzipped tarball written while it's read, so the archive is never in memory.
func streamedTarball(t *testing.T, entries ...tarEntry) io.Reader {
	t.Helper()
	r, w := io.Pipe()
	go func() { _ = w.CloseWithError(writeTarball(w, entries...)) }()
	t.Cleanup(func() { _ = r.Close() }) // unblocks the writer once the scan stops reading
	return r
}

func TestParseManifestFromTarball(t *testing.T) {
	manifest := tarEntry{name: "convox.yml", body: []byte(webManifest)}
	source := tarEntry{name: "src/app.rb", body: []byte("puts 1\n")}
	staging := tarEntry{name: "deploy/convox.staging.yml", body: []byte(webManifest)}
	noServices := []byte("services: {}\n")
	cases := []struct {
		name         string
		manifestPath string
		archive      []byte
		wantErr      string
	}{
		{
			name:    "finds the manifest among the source",
			archive: gzippedTarball(t, tarEntry{name: "src/", typeflag: tar.TypeDir}, source, manifest),
		},
		{
			name:    "matches the manifest by its cleaned path",
			archive: gzippedTarball(t, tarEntry{name: "./convox.yml", body: []byte(webManifest)}),
		},
		{
			name:         "reads a manifest at another path",
			manifestPath: "deploy/convox.staging.yml",
			archive:      gzippedTarball(t, tarEntry{name: "convox.yml", body: noServices}, staging),
		},
		{
			name:    "reports a missing manifest",
			archive: gzippedTarball(t, source),
			wantErr: "convox.yml not found in tarball",
		},
		{
			// The rack would write the second over the first, so the first isn't the manifest it builds from.
			name:    "refuses a second manifest",
			archive: gzippedTarball(t, manifest, source, tarEntry{name: "./convox.yml", body: noServices}),
			wantErr: "tarball contains convox.yml more than once",
		},
		{
			name: "refuses a manifest that isn't a regular file",
			archive: gzippedTarball(t,
				tarEntry{name: "convox.yml", typeflag: tar.TypeSymlink, linkname: "other.yml"},
				tarEntry{name: "other.yml", body: []byte(webManifest)},
			),
			wantErr: "convox.yml in tarball is not a regular file",
		},
		{
			name:    "refuses path traversal",
			archive: gzippedTarball(t, tarEntry{name: "../escape.yml", body: []byte(webManifest)}, manifest),
			wantErr: "path traversal not allowed",
		},
		{
			name:    "refuses absolute paths",
			archive: gzippedTarball(t, tarEntry{name: "/etc/convox.yml", body: []byte(webManifest)}, manifest),
			wantErr: "absolute paths not allowed",
		},
		{
			name:    "refuses an oversized file",
			archive: gzippedTarball(t, tarEntry{name: "big.bin", body: make([]byte, maxFileSize+1)}, manifest),
			wantErr: "file big.bin too large",
		},
		{
			name:    "refuses an oversized manifest",
			archive: gzippedTarball(t, tarEntry{name: "convox.yml", body: make([]byte, maxManifestSize+1)}),
			wantErr: "manifest too large",
		},
		{
			// Parsing builds a node per scalar before anything is checked, so a manifest is capped well below the
			// size at which a flow sequence of one-character scalars would exhaust the gateway's memory.
			name:    "refuses a 200 KB manifest",
			archive: gzippedTarball(t, tarEntry{name: "convox.yml", body: flowSequenceManifest(200 * 1024)}),
			wantErr: "manifest too large",
		},
		{
			name:    "refuses data that isn't gzipped",
			archive: []byte("not a tarball"),
			wantErr: "failed to create gzip reader",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifestPath := tc.manifestPath
			if manifestPath == "" {
				manifestPath = defaultManifestPath
			}
			got, err := parseArchiveManifest(t, bytes.NewReader(tc.archive), manifestPath)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "docker.io/org/app:abc", got.Services["web"].Image)
		})
	}
}

// The manifest is read from the tarball as it streams in: here the archive is written while it's read, and never
// exists in full. The scan carries on past the manifest to the end of the archive, so a second manifest entry after
// tens of megabytes of other files is still caught.
func TestManifestScanStreamsTheWholeTarball(t *testing.T) {
	const paddingFiles = 4
	padding := make([]byte, maxFileSize)
	entries := make([]tarEntry, 0, 1+paddingFiles)
	entries = append(entries, tarEntry{name: "convox.yml", body: []byte(webManifest)})
	for i := range paddingFiles {
		entries = append(entries, tarEntry{name: fmt.Sprintf("assets/%d.bin", i), body: padding})
	}

	manifest, err := parseArchiveManifest(t, streamedTarball(t, entries...), defaultManifestPath)
	require.NoError(t, err)
	require.Equal(t, "docker.io/org/app:abc", manifest.Services["web"].Image)

	replacement := tarEntry{name: "convox.yml", body: []byte("services:\n  web:\n    image: attacker/app\n")}
	_, err = parseArchiveManifest(t, streamedTarball(t, append(entries, replacement)...), defaultManifestPath)
	require.ErrorContains(t, err, "tarball contains convox.yml more than once")
}

// The size limit can fall exactly between two gzip members, where gzip and tar both see a normal end of the archive.
// The scan must still refuse it: the rack extracts every member, and a later one can hold another manifest.
func TestManifestScanRefusesAnArchiveThatReachesTheSizeLimit(t *testing.T) {
	var first, second bytes.Buffer
	require.NoError(t, writeGzippedTar(&first, false, tarEntry{name: "convox.yml", body: []byte(webManifest)}))
	replacement := tarEntry{name: "convox.yml", body: []byte("services:\n  web:\n    image: attacker/app\n")}
	require.NoError(t, writeGzippedTar(&second, true, replacement))
	archive := append(append([]byte{}, first.Bytes()...), second.Bytes()...)

	_, err := readArchiveManifest(bytes.NewReader(archive), defaultManifestPath, int64(first.Len()))
	require.ErrorContains(t, err, "tarball exceeds maximum size")

	// Read in full, the same archive has two manifests.
	_, err = readArchiveManifest(bytes.NewReader(archive), defaultManifestPath, int64(len(archive)+1))
	require.ErrorContains(t, err, "tarball contains convox.yml more than once")
}

// The tar reader reads each entry's metadata (PAX records, up to 1MiB each) before the scan sees the entry, so the
// uncompressed stream is bounded as a whole.
func TestManifestScanBoundsTheUncompressedStream(t *testing.T) {
	padding := map[string]string{"RGW.padding": strings.Repeat("x", 600*1024)}
	var archive bytes.Buffer
	require.NoError(t, writeTarball(&archive,
		tarEntry{name: "a", pax: padding}, tarEntry{name: "b", pax: padding}, tarEntry{name: "c", pax: padding},
		tarEntry{name: "convox.yml", body: []byte(webManifest)}))

	_, err := readManifestFromTarball(bytes.NewReader(archive.Bytes()), defaultManifestPath, 1024*1024)
	require.ErrorContains(t, err, "tarball expands to more than 1048576 bytes")

	data, err := readManifestFromTarball(bytes.NewReader(archive.Bytes()), defaultManifestPath, 4*1024*1024)
	require.NoError(t, err)
	require.Equal(t, webManifest, string(data))
}

// parseArchiveManifest reads and parses the manifest in an archive, as a build's check does.
func parseArchiveManifest(t *testing.T, r io.Reader, manifestPath string) (*convoxManifest, error) {
	t.Helper()
	data, err := readArchiveManifest(r, manifestPath, maxTarballSize)
	if err != nil {
		return nil, err
	}
	return parseManifest(data)
}
