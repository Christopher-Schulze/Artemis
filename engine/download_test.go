package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineDownloadUsesOwnedSessionPathAndResponseFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="proof.txt"`)
		writeTestBody(t, w, "download-proof")
	}))
	defer server.Close()
	root := t.TempDir()
	config := testConfig(server)
	config.SessionID = "engine-session"
	config.DownloadRoot = root
	config.PolicyConfig.AllowedDownloadTypes = []string{"text/plain"}
	config.PolicyConfig.MaxDownloadBytes = 64
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	download, err := engine.Download(context.Background(), server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(canonicalRoot, "engine-session", "downloads", "proof.txt")
	if download.Path != wantPath || download.Filename != "proof.txt" || download.MIME != "text/plain" || download.SHA256 == "" {
		t.Fatalf("download=%#v want path=%s", download, wantPath)
	}
	content, err := os.ReadFile(download.Path)
	if err != nil || string(content) != "download-proof" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestPageSaveDownloadUsesSamePolicyBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, "<!doctype html><title>Saved</title>")
	}))
	defer server.Close()
	config := testConfig(server)
	config.SessionID = "page-session"
	config.DownloadRoot = t.TempDir()
	config.PolicyConfig.AllowedDownloadTypes = []string{"text/html"}
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	page, err := engine.Fetch(context.Background(), server.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	download, err := page.SaveDownload("page.html")
	if err != nil || !strings.HasSuffix(download.Path, filepath.Join("page-session", "downloads", "page.html")) {
		t.Fatalf("download=%#v err=%v", download, err)
	}
	if _, err := page.SaveDownload("../escape.html"); err == nil {
		t.Fatal("page download traversal accepted")
	}
}

func TestEngineDownloadRequiresSessionWithoutLeavingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestBody(t, w, "content")
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "not-created")
	config := testConfig(server)
	config.DownloadRoot = root
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	if _, err := engine.Download(context.Background(), server.URL, "file.txt"); err == nil || !strings.Contains(err.Error(), "session ID") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("download root unexpectedly created: %v", err)
	}
}
