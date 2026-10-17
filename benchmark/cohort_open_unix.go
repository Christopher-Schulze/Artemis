//go:build darwin || linux

package benchmark

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openCohortMember(root *os.Root, name string) (file *os.File, returnErr error) {
	if root == nil || !filepath.IsLocal(name) || filepath.Clean(name) != name {
		return nil, fmt.Errorf("artifact: canonical local path required")
	}
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, parent.Close())
		if returnErr != nil && file != nil {
			returnErr = errors.Join(returnErr, file.Close())
			file = nil
		}
	}()
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, directory.Close()) }()
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(name), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file = os.NewFile(uintptr(fd), name)
	if file == nil {
		return nil, errors.Join(fmt.Errorf("artifact: invalid opened descriptor"), unix.Close(fd))
	}
	return file, nil
}
