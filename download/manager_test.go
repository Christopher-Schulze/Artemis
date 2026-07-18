package download

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestDownloadManagerStoresVerifiedFileAtomically(t *testing.T) {
	manager := newTestManager(t, 64, 128, []string{"text/plain"})
	download, err := manager.Store("proof.txt", "application/octet-stream", []byte("verified-download"))
	if err != nil {
		t.Fatal(err)
	}
	if download.Filename != "proof.txt" || download.MIME != "text/plain" || download.Size != 17 || len(download.SHA256) != 64 {
		t.Fatalf("download=%#v", download)
	}
	content, err := os.ReadFile(download.Path)
	if err != nil || string(content) != "verified-download" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	info, err := os.Stat(download.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	assertNoPartialFiles(t, manager.Directory())
}

func TestDownloadManagerRejectsTraversalAndSymlinkSession(t *testing.T) {
	manager := newTestManager(t, 64, 128, []string{"*/*"})
	for _, target := range []string{"../escape.txt", "nested/escape.txt", filepath.Join(filepath.Dir(manager.Directory()), "outside.txt")} {
		if _, err := manager.ResolveTarget(target); err == nil {
			t.Fatalf("ResolveTarget(%q) accepted", target)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	policy := newTestPolicy(t, 64, []string{"*/*"})
	if _, err := NewDownloadManager(DownloadConfig{RootDir: root, SessionID: "linked", Policy: policy}); err == nil {
		t.Fatal("symlink session directory accepted")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: entries=%d err=%v", len(entries), err)
	}
}

func TestDownloadManagerEnforcesTypeSizeQuotaAndHeadroom(t *testing.T) {
	tests := []struct {
		name    string
		manager func(*testing.T) *DownloadManager
		content []byte
		want    string
	}{
		{name: "type", manager: func(t *testing.T) *DownloadManager { return newTestManager(t, 64, 128, []string{"image/png"}) }, content: []byte("plain text"), want: "content_type"},
		{name: "size", manager: func(t *testing.T) *DownloadManager { return newTestManager(t, 4, 128, []string{"*/*"}) }, content: []byte("12345"), want: "too_large"},
		{name: "headroom", manager: func(t *testing.T) *DownloadManager {
			policy := newTestPolicy(t, 64, []string{"*/*"})
			manager, err := NewDownloadManager(DownloadConfig{RootDir: t.TempDir(), SessionID: "headroom", MaxDiskBytes: 128, MinFreeBytes: 1 << 62, Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			return manager
		}, content: []byte("data"), want: "headroom"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := test.manager(t)
			_, err := manager.Store("blocked.bin", "", test.content)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
			assertDirectoryEmpty(t, manager.Directory())
		})
	}

	manager := newTestManager(t, 64, 10, []string{"*/*"})
	if _, err := manager.Store("first.txt", "", []byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Store("second.txt", "", []byte("123456")); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("quota error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.Directory(), "second.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected target remains: %v", err)
	}
}

func TestDownloadManagerAdoptsAndCleansBrowserFiles(t *testing.T) {
	manager := newTestManager(t, 8, 16, []string{"text/plain"})
	stage, err := manager.NewBrowserStage()
	if err != nil {
		t.Fatal(err)
	}
	stageDir := stage.Directory()
	path := filepath.Join(stageDir, "browser.txt")
	if err := os.WriteFile(path, []byte("browser"), 0o600); err != nil {
		t.Fatal(err)
	}
	download, err := stage.Adopt("browser.txt", "application/octet-stream")
	if err != nil || download.SHA256 == "" {
		t.Fatalf("download=%#v err=%v", download, err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed stage remains: %v", err)
	}
	rejectedStage, err := manager.NewBrowserStage()
	if err != nil {
		t.Fatal(err)
	}
	rejectedDir := rejectedStage.Directory()
	oversize := filepath.Join(rejectedDir, "oversize.txt")
	if err := os.WriteFile(oversize, []byte("too-large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rejectedStage.Adopt("oversize.txt", ""); err == nil {
		t.Fatal("oversize browser file accepted")
	}
	if err := rejectedStage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rejectedDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected stage remains: %v", err)
	}
	entries, err := os.ReadDir(manager.Directory())
	if err != nil || len(entries) != 1 || entries[0].Name() != "browser.txt" {
		t.Fatalf("committed entries=%v err=%v", entries, err)
	}
}

func TestDownloadManagerConcurrentSameTargetNeverOverwrites(t *testing.T) {
	manager := newTestManager(t, 64, 128, []string{"text/plain"})
	contents := [][]byte{[]byte("first"), []byte("second")}
	errorsByAttempt := make([]error, len(contents))
	var wait sync.WaitGroup
	for index := range contents {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errorsByAttempt[index] = manager.Store("same.txt", "", contents[index])
		}(index)
	}
	wait.Wait()
	successes := 0
	for _, err := range errorsByAttempt {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d errors=%v", successes, errorsByAttempt)
	}
	stored, err := os.ReadFile(filepath.Join(manager.Directory(), "same.txt"))
	if err != nil || string(stored) != "first" && string(stored) != "second" {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
	assertNoPartialFiles(t, manager.Directory())
}

func newTestManager(t *testing.T, maxFile, maxDisk int64, allowed []string) *DownloadManager {
	t.Helper()
	manager, err := NewDownloadManager(DownloadConfig{
		RootDir:      t.TempDir(),
		SessionID:    "test-session",
		MaxDiskBytes: maxDisk,
		Policy:       newTestPolicy(t, maxFile, allowed),
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func newTestPolicy(t *testing.T, maxFile int64, allowed []string) *network.Policy {
	t.Helper()
	policy, err := network.NewPolicy(network.PolicyConfig{MaxDownloadBytes: maxFile, AllowedDownloadTypes: allowed}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func assertDirectoryEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func assertNoPartialFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".partial") {
			t.Fatalf("partial file remains: %s", entry.Name())
		}
	}
}
