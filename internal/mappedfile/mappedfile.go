package mappedfile

import (
	"io"
	"os"
)

// Open memory-maps the file at path read-only when the platform supports it.
// Mapped pages are backed by the OS page cache and can be reclaimed under
// memory pressure instead of counting against the app's anonymous heap, which
// significantly lowers peak RAM for large game archives. If mapping is
// unavailable it falls back to reading the whole file into memory.
func Open(path string) ([]byte, io.Closer, error) {
	if m, f, err := mmapFile(path); err == nil && m != nil {
		return m, f, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return data, nopCloser{}, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }