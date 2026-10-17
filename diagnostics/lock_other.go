//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package diagnostics

import (
	"fmt"
	"os"
)

type fileLock struct {
	file *os.File
}

// openNoFollow has no portable equivalent here; persistent stores are
// refused below before any path is opened.
const openNoFollow = 0

// acquireFileLock fails closed: without OS advisory locking a persistent
// ledger cannot serialize writers across processes.
func acquireFileLock(path string) (*fileLock, error) {
	return nil, fmt.Errorf("diagnostics lock %s: persistent diagnostics require OS file locking, unsupported on this platform", path)
}

func (l *fileLock) release() {
	if l != nil && l.file != nil {
		_ = l.file.Close()
	}
}
