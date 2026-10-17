//go:build darwin || linux

package fixture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openReplayFile(root *os.Root, name string) (file *os.File, returnErr error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, fmt.Errorf("replay: one local file component required")
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, directory.Close())
		if returnErr != nil && file != nil {
			returnErr = errors.Join(returnErr, file.Close())
			file = nil
		}
	}()
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file = os.NewFile(uintptr(fd), name)
	if file == nil {
		return nil, errors.Join(fmt.Errorf("replay: opened descriptor is invalid"), unix.Close(fd))
	}
	return file, nil
}
