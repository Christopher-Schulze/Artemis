package wpt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

// EmbeddedAssetManifest returns a deterministic aggregate of the pinned WPT
// subset embedded into Artemis. Paths and file boundaries are included in the
// digest, so renames and concatenation collisions are detected.
func EmbeddedAssetManifest() (int64, string, error) {
	paths := make([]string, 0)
	err := fs.WalkDir(testdata, "testdata/wpt", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return 0, "", fmt.Errorf("walk embedded WPT subset: %w", err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	var size int64
	for _, path := range paths {
		data, err := testdata.ReadFile(path)
		if err != nil {
			return 0, "", fmt.Errorf("read embedded WPT asset %s: %w", path, err)
		}
		if _, err := fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(data)); err != nil {
			return 0, "", err
		}
		if _, err := hash.Write(data); err != nil {
			return 0, "", err
		}
		size += int64(len(data))
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
