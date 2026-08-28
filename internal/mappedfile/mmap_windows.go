//go:build windows

package mappedfile

import (
	"errors"
	"io"
)

func mmapFile(path string) ([]byte, io.Closer, error) {
	return nil, nil, errors.New("mmap unsupported on windows")
}