//go:build !darwin && !linux

package main

import "errors"

func renameExclusive(string, string) error {
	return errors.New("atomic exclusive release publication requires Darwin or Linux")
}
