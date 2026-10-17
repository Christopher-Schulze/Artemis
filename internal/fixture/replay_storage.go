package fixture

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func publishReplay(dir, filename string, data []byte) (path string, returnErr error) {
	if dir == "" {
		return "", fmt.Errorf("replay: output directory is required")
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	root, err := openReplayDirectory(dir)
	if err != nil {
		return "", err
	}
	defer func() {
		returnErr = errors.Join(returnErr, root.Close())
		if returnErr != nil {
			path = ""
		}
	}()
	stage, identity, err := stageReplay(root, data)
	if err != nil {
		return "", err
	}
	defer func() {
		returnErr = errors.Join(returnErr, removeOwnedReplayStage(root, stage, identity), syncReplayRoot(root))
	}()
	if err := root.Link(stage, filename); err != nil {
		return "", fmt.Errorf("replay: exclusive publication: %w", err)
	}
	published, err := root.Lstat(filename)
	if err != nil || !os.SameFile(identity, published) {
		return "", errors.Join(err, fmt.Errorf("replay: published identity changed"))
	}
	if err := errors.Join(syncReplayRoot(root), syncReplayParents(dir)); err != nil {
		return "", err
	}
	if err := verifyReplayDirectoryPath(root, dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func verifyReplayDirectoryPath(root *os.Root, dir string) error {
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	current, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !current.IsDir() || !os.SameFile(opened, current) || current.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("replay: publication directory path changed")
	}
	return nil
}

func openReplayDirectory(dir string) (root *os.Root, returnErr error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("replay: real output directory without group/other write access required")
	}
	root, err = os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0o022 != 0 {
		return nil, errors.Join(err, fmt.Errorf("replay: output directory identity changed"), root.Close())
	}
	return root, nil
}

func stageReplay(root *os.Root, data []byte) (name string, identity os.FileInfo, returnErr error) {
	name = ".replay-stage-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, err
	}
	identity, err = file.Stat()
	if err != nil {
		return "", nil, errors.Join(err, file.Close())
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
		if returnErr != nil {
			returnErr = errors.Join(returnErr, removeOwnedReplayStage(root, name, identity), syncReplayRoot(root))
		}
	}()
	if _, err := file.Write(data); err != nil {
		return name, identity, err
	}
	return name, identity, file.Sync()
}

func removeOwnedReplayStage(root *os.Root, name string, identity os.FileInfo) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(identity, info) || !info.Mode().IsRegular() {
		return fmt.Errorf("replay: refuse removal of changed stage identity")
	}
	return root.Remove(name)
}

func syncReplayRoot(root *os.Root) (returnErr error) {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	return file.Sync()
}

func syncReplayParents(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for parent := filepath.Dir(absolute); ; parent = filepath.Dir(parent) {
		root, err := os.OpenRoot(parent)
		if err != nil {
			return err
		}
		if err := errors.Join(syncReplayRoot(root), root.Close()); err != nil {
			return err
		}
		if filepath.Dir(parent) == parent {
			return nil
		}
	}
}

func readReplayBytes(path string) (data []byte, returnErr error) {
	root, err := openReplayDirectory(filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, root.Close())
		if returnErr != nil {
			data = nil
		}
	}()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || before.Size() > maxReplayBytes {
		return nil, fmt.Errorf("replay: bounded private regular file required")
	}
	return readPinnedReplayFile(root, path, before)
}

func readPinnedReplayFile(root *os.Root, path string, before os.FileInfo) (data []byte, returnErr error) {
	file, err := openReplayFile(root, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
		if returnErr != nil {
			data = nil
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 || opened.Size() > maxReplayBytes {
		return nil, errors.Join(err, fmt.Errorf("replay: opened file identity changed"))
	}
	data, err = io.ReadAll(io.LimitReader(file, maxReplayBytes+1))
	if err != nil || len(data) > maxReplayBytes || int64(len(data)) != opened.Size() {
		return nil, errors.Join(err, fmt.Errorf("replay: encoded bytes exceeded limit or changed during read"))
	}
	if err := verifyReplayRead(root, file, path, opened); err != nil {
		return nil, err
	}
	return data, nil
}

func verifyReplayRead(root *os.Root, file *os.File, path string, opened os.FileInfo) error {
	current, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := root.Lstat(filepath.Base(path))
	if err != nil {
		return err
	}
	for _, info := range []os.FileInfo{current, named} {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !os.SameFile(opened, info) ||
			info.Size() != opened.Size() || !info.ModTime().Equal(opened.ModTime()) {
			return fmt.Errorf("replay: file identity, permissions or content changed during read")
		}
	}
	return verifyReplayDirectoryPath(root, filepath.Dir(filepath.Clean(path)))
}
