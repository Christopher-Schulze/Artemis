//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package diagnostics

import (
	"fmt"
	"os"
)

type fileLock struct {
	file *os.File
}

func acquireFileLock(path string) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("diagnostics lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("diagnostics lock permissions: %w", err)
	}
	return &fileLock{file: file}, nil
}

func (l *fileLock) release() {
	if l != nil && l.file != nil {
		_ = l.file.Close()
	}
}
