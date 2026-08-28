//go:build !windows

package mappedfile

import (
	"io"
	"os"
	"syscall"
)

func mmapFile(path string) ([]byte, io.Closer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil || st.Size() <= 0 {
		f.Close()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, nil, err
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return b, &mmapCloser{file: f, data: b}, nil
}

type mmapCloser struct {
	file *os.File
	data []byte
}

func (c *mmapCloser) Close() error {
	var err error
	if c.data != nil {
		err = syscall.Munmap(c.data)
	}
	if c.file != nil {
		if e := c.file.Close(); err == nil {
			err = e
		}
	}
	return err
}