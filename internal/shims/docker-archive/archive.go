package archive

import (
	"io"

	mobyarchive "github.com/moby/go-archive"
	"github.com/moby/go-archive/compression"
)

type (
	Compression = compression.Compression
	TarOptions  = mobyarchive.TarOptions
)

const (
	Uncompressed = compression.None
	Bzip2        = compression.Bzip2
	Gzip         = compression.Gzip
	Xz           = compression.Xz
	Zstd         = compression.Zstd
)

// Tar creates an archive from path using the current Moby archive package.
func Tar(path string, comp Compression) (io.ReadCloser, error) {
	return mobyarchive.Tar(path, comp)
}

// TarWithOptions creates an archive using the current Moby archive package.
func TarWithOptions(srcPath string, options *TarOptions) (io.ReadCloser, error) {
	return mobyarchive.TarWithOptions(srcPath, options)
}
