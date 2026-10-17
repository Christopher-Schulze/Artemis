package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestVerifiedDownloadIsBoundedAtomicAndClean(t *testing.T) {
	body := []byte("pinned transfer fixture")
	cases := []struct {
		name      string
		status    int
		limit     int64
		digest    string
		chunked   bool
		wantError bool
	}{
		{"valid", 200, 1024, fmt.Sprintf("%x", sha256.Sum256(body)), false, false},
		{"wrong digest", 200, 1024, strings.Repeat("a", 64), false, true},
		{"declared oversize", 200, 4, fmt.Sprintf("%x", sha256.Sum256(body)), false, true},
		{"chunked oversize", 200, 4, fmt.Sprintf("%x", sha256.Sum256(body)), true, true},
		{"HTTP error", 503, 1024, fmt.Sprintf("%x", sha256.Sum256(body)), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(tc.status)
				if tc.chunked {
					w.(http.Flusher).Flush()
				}
				if _, err := w.Write(body); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			dir := t.TempDir()
			runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: dir, DownloadURL: srv.URL, ExpectedSHA256: tc.digest, MaxDownloadBytes: tc.limit})
			err := runner.EnsureBinary(t.Context())
			if (err != nil) != tc.wantError {
				t.Fatalf("download error = %v, want error=%v", err, tc.wantError)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError {
				if len(entries) != 0 {
					t.Fatalf("failed transfer left artifacts: %v", entries)
				}
				return
			}
			actual, err := os.ReadFile(runner.BinaryPath())
			if err != nil || !bytes.Equal(actual, body) || len(entries) != 1 || runner.VerifiedSHA256() != tc.digest {
				t.Fatalf("published bytes, digest or cleanup wrong: %q, %v", actual, err)
			}
		})
	}
}

func TestUnverifiedAndCancelledDownloadHaveNoSideEffects(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { requests.Add(1) }))
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "not-created")
	runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: dir, DownloadURL: srv.URL})
	if err := runner.EnsureBinary(t.Context()); err == nil {
		t.Fatal("unpinned transfer admitted")
	}
	if requests.Load() != 0 {
		t.Fatal("unverified transfer contacted server")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("unverified transfer created output")
	}
	runner.cfg.ExpectedSHA256 = strings.Repeat("a", 64)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runner.EnsureBinary(ctx); err == nil {
		t.Fatal("cancelled transfer admitted")
	}
	if requests.Load() != 0 {
		t.Fatal("cancelled transfer contacted server")
	}
}

func TestCachedCompetitorRequiresPinnedDigestAndPreservesOperatorBytes(t *testing.T) {
	dir := t.TempDir()
	runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: dir})
	original := []byte("operator-owned binary fixture")
	if err := os.WriteFile(runner.BinaryPath(), original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runner.EnsureBinary(t.Context()); err == nil {
		t.Fatal("unverified cached executable admitted")
	}
	runner.cfg.ExpectedSHA256 = strings.Repeat("a", 64)
	if err := runner.EnsureBinary(t.Context()); err == nil {
		t.Fatal("wrong cached digest admitted")
	}
	actual, err := os.ReadFile(runner.BinaryPath())
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("operator bytes changed")
	}
	runner.cfg.ExpectedSHA256 = fmt.Sprintf("%x", sha256.Sum256(original))
	if err := runner.EnsureBinary(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestExecutedIdentityRejectsReplacementWithIdenticalBytes(t *testing.T) {
	dir := t.TempDir()
	body := []byte("verified identity fixture")
	runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: dir, ExpectedSHA256: fmt.Sprintf("%x", sha256.Sum256(body))})
	if err := os.WriteFile(runner.BinaryPath(), body, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runner.EnsureBinary(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(runner.BinaryPath(), filepath.Join(dir, "preserved-original")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner.BinaryPath(), body, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runner.verifyExecutedFile(); err == nil {
		t.Fatal("replacement inode admitted as executed provenance")
	}
}

func TestDownloadFailureRedactsQueryCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: t.TempDir(), DownloadURL: srv.URL + "?token=must-not-leak", ExpectedSHA256: strings.Repeat("a", 64)})
	err := runner.EnsureBinary(t.Context())
	if err == nil || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("secret-bearing transport error: %v", err)
	}
}
