//go:build darwin

package main

import "golang.org/x/sys/unix"

func renameExclusive(oldPath, newPath string) error {
	return unix.RenamexNp(oldPath, newPath, unix.RENAME_EXCL)
}
