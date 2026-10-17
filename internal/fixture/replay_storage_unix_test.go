//go:build darwin || linux

package fixture

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReplayNoFollowOpenCannotBlockOnFIFOOrReadSymlink(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := unix.Mkfifo(filepath.Join(dir, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openReplayFile(root, "fifo")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := file.Stat(); err != nil || info.Mode().IsRegular() {
		t.Fatalf("FIFO became regular: %v %v", info, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readReplayBytes(filepath.Join(dir, "fifo")); err == nil {
		t.Fatal("FIFO admitted as evidence")
	}
	if err := os.WriteFile(filepath.Join(dir, "actual"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("actual", "alias"); err != nil {
		t.Fatal(err)
	}
	file, err = openReplayFile(root, "alias")
	if file != nil {
		t.Fatal(errors.Join(err, file.Close()))
	}
	if err == nil {
		t.Fatal("no-follow open accepted a symlink")
	}
}

func TestReplayPinnedDirectoryDetectsReplacementAndPreservesForeignStage(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "owned")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := openReplayDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	name, identity, err := stageReplay(root, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Rename(name, name+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(name, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeOwnedReplayStage(root, name, identity); err == nil {
		t.Fatal("foreign replacement was treated as an owned stage")
	}
	data, err := root.ReadFile(name)
	if err != nil || string(data) != "foreign" {
		t.Fatalf("foreign replacement changed: %q %v", data, err)
	}
	if err := os.Rename(dir, dir+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyReplayDirectoryPath(root, dir); err == nil {
		t.Fatal("replacement path acknowledged as original directory")
	}
}

func TestReplayReadCompletionRejectsChangedFileAndDirectory(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(string, os.FileInfo) error
	}{
		{name: "unchanged"},
		{name: "replaced file", mutate: func(path string, _ os.FileInfo) error {
			if err := os.Rename(path, path+"-original"); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("original"), 0o600)
		}},
		{name: "symlink replacement", mutate: func(path string, _ os.FileInfo) error {
			if err := os.Rename(path, path+"-original"); err != nil {
				return err
			}
			return os.Symlink(filepath.Base(path)+"-original", path)
		}},
		{name: "changed file permissions", mutate: func(path string, _ os.FileInfo) error {
			return os.Chmod(path, 0o644)
		}},
		{name: "changed size", mutate: func(path string, _ os.FileInfo) error {
			return os.WriteFile(path, []byte("longer replacement"), 0o600)
		}},
		{name: "same size rewrite", mutate: func(path string, opened os.FileInfo) error {
			if err := os.WriteFile(path, []byte("modified"), 0o600); err != nil {
				return err
			}
			return os.Chtimes(path, time.Time{}, opened.ModTime().Add(-time.Hour))
		}},
		{name: "changed directory permissions", mutate: func(path string, _ os.FileInfo) error {
			return os.Chmod(filepath.Dir(path), 0o777)
		}},
		{name: "replaced directory", mutate: func(path string, _ os.FileInfo) error {
			dir := filepath.Dir(path)
			if err := os.Rename(dir, dir+"-original"); err != nil {
				return err
			}
			return os.Mkdir(dir, 0o700)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "owned")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "capture.json")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			root, err := openReplayDirectory(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			file, err := openReplayFile(root, filepath.Base(path))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := file.Close(); err != nil {
					t.Error(err)
				}
			})
			opened, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			if err != nil || string(data) != "original" {
				t.Fatalf("actual owned bytes required: %q %v", data, err)
			}
			if test.mutate != nil {
				if err := test.mutate(path, opened); err != nil {
					t.Fatal(err)
				}
			}
			err = verifyReplayRead(root, file, path, opened)
			if test.mutate != nil {
				if err == nil {
					t.Fatal("changed replay filesystem state acknowledged")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := readReplayBytes(path)
			if err != nil || string(loaded) != string(data) {
				t.Fatalf("unchanged loader bytes differ: %q %v", loaded, err)
			}
		})
	}
}
