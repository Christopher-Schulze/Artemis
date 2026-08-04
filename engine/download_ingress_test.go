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

type recordingDownloadIngress struct {
	sessionID   string
	filename    string
	contentType string
	content     []byte
	err         error
}

func (r *recordingDownloadIngress) PublishDownload(_ context.Context, sessionID, filename, contentType string, content []byte) error {
	r.sessionID = sessionID
	r.filename = filename
	r.contentType = contentType
	r.content = append([]byte(nil), content...)
	return r.err
}

func TestEngineDownloadPublishesThroughGovernedIngress(t *testing.T) {
	const body = "downloaded bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	ingress := &recordingDownloadIngress{}
	cfg := testConfig(srv)
	cfg.DownloadRoot = filepath.Join(t.TempDir(), "downloads")
	cfg.SessionID = "session-1"
	cfg.DownloadIngress = ingress
	cfg.RequireDownloadIngress = true
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	download, err := eng.Download(context.Background(), srv.URL, "payload.bin")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if download == nil || ingress.sessionID != "session-1" || ingress.filename != "payload.bin" {
		t.Fatalf("download ingress metadata=%+v download=%+v", ingress, download)
	}
	if string(ingress.content) != body || ingress.contentType != "application/octet-stream" {
		t.Fatalf("download ingress payload=%q type=%q", ingress.content, ingress.contentType)
	}
}

func TestEngineRejectsRequiredDownloadIngressWithoutBoundary(t *testing.T) {
	cfg := Config{RequireDownloadIngress: true}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "engine: download ingress required") {
		t.Fatalf("required download ingress error=%v", err)
	}
}

func TestEngineRemovesDownloadWhenIngressRejects(t *testing.T) {
	const body = "rejected download"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	root := filepath.Join(t.TempDir(), "downloads")
	ingress := &recordingDownloadIngress{err: errors.New("publisher unavailable")}
	cfg := testConfig(srv)
	cfg.DownloadRoot = root
	cfg.SessionID = "session-reject"
	cfg.DownloadIngress = ingress
	cfg.RequireDownloadIngress = true
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	if _, err := eng.Download(context.Background(), srv.URL, "payload.bin"); err == nil {
		t.Fatal("expected governed ingress rejection")
	}
	path := filepath.Join(root, "session-reject", "downloads", "payload.bin")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected download remained at %s: %v", path, err)
	}
}

func TestEngineRejectsRequiredDownloadReservationWithoutAuthority(t *testing.T) {
	cfg := Config{RequireDownloadReservation: true}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "engine: download reservation required") {
		t.Fatalf("required download reservation error=%v", err)
	}
}
