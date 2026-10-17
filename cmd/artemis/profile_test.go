package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileListRejectsFailedMetadataWithoutChangingIt(t *testing.T) {
	for _, directory := range []bool{false, true} {
		root := t.TempDir()
		path := filepath.Join(root, "profiles.json")
		if directory {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, raw := captureCLIOutput(t, func() int { return cmdProfile([]string{"--root", root, "--owner", "alice"}) })
		if code != 1 || len(raw) != 0 {
			t.Fatalf("failed metadata became a successful list: directory=%v code=%d stdout=%s", directory, code, raw)
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() != directory {
			t.Fatalf("metadata identity changed: %v", err)
		}
		if !directory {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "{" {
				t.Fatalf("corrupt metadata was modified: %s err=%v", data, err)
			}
		}
	}
}
