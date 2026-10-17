package benchmark

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func writeArtifact(path string, data []byte) (returnErr error) {
	if int64(len(data)) > MaxArtifactFileBytes {
		return fmt.Errorf("benchmark artifact exceeds file byte limit")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact requires a real output directory: %s", directory)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("artifact output directory changed")
	}
	return writeArtifactInRoot(root, filepath.Base(path), data)
}

func writeArtifactBytes(file *os.File, data []byte) (returnErr error) {
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func writeArtifactInRoot(root *os.Root, destination string, data []byte) (returnErr error) {
	if root == nil || !filepath.IsLocal(destination) || filepath.Base(destination) != destination || destination == "." || int64(len(data)) > MaxArtifactFileBytes {
		return fmt.Errorf("artifact publication requires a local filename")
	}
	name := ".benchmark-artifact-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	identity, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	defer func() { returnErr = errors.Join(returnErr, removeArtifactStage(root, name, identity)) }()
	if err := writeArtifactBytes(file, data); err != nil {
		return err
	}
	if err := root.Link(name, destination); err != nil {
		return fmt.Errorf("publish artifact without replacement: %w", err)
	}
	return nil
}

func removeArtifactStage(root *os.Root, name string, identity os.FileInfo) error {
	current, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(current, identity) {
		return fmt.Errorf("artifact: stage identity changed; retained unexpected contents")
	}
	return root.Remove(name)
}
