package benchmark

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type cohortContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r cohortContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func digestArtifact(ctx context.Context, root *os.Root, name string, maximum int64, capture io.Writer) (result ArtifactDigest, returnErr error) {
	file, opened, err := openCohortFile(ctx, root, name, maximum)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	hash := sha256.New()
	var destination io.Writer = hash
	if capture != nil {
		destination = io.MultiWriter(hash, capture)
	}
	written, err := io.Copy(destination, cohortContextReader{ctx: ctx, reader: io.LimitReader(file, maximum+1)})
	if err != nil {
		return result, err
	}
	if written > maximum || written != opened.Size() {
		return result, fmt.Errorf("artifact size changed: %s", name)
	}
	if err := verifyCohortFile(root, file, name, opened); err != nil {
		return result, err
	}
	return ArtifactDigest{Path: name, SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Bytes: written}, ctx.Err()
}

func openCohortFile(ctx context.Context, root *os.Root, name string, maximum int64) (*os.File, os.FileInfo, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("artifact: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if root == nil || !filepath.IsLocal(name) || filepath.Clean(name) != name || maximum < 0 || maximum > MaxArtifactFileBytes {
		return nil, nil, fmt.Errorf("artifact path or byte bound denied")
	}
	identity, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !identity.Mode().IsRegular() || identity.Size() < 0 || identity.Size() > maximum {
		return nil, nil, fmt.Errorf("artifact must be a bounded regular file: %s", name)
	}
	file, err := openCohortMember(root, name)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(identity, opened) || opened.Size() != identity.Size() {
		return nil, nil, errors.Join(fmt.Errorf("artifact changed while opening: %s", name), err, file.Close())
	}
	return file, opened, nil
}

func verifyCohortFile(root *os.Root, file *os.File, name string, identity os.FileInfo) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !named.Mode().IsRegular() || !os.SameFile(named, identity) || !os.SameFile(opened, identity) ||
		opened.Size() != identity.Size() || named.Size() != identity.Size() || !opened.ModTime().Equal(identity.ModTime()) {
		return fmt.Errorf("artifact changed during read: %s", name)
	}
	return nil
}
