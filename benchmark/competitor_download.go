package benchmark

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func (r *CompetitorRunner) downloadBinary(ctx context.Context) (returnErr error) {
	if err := os.MkdirAll(r.cfg.BinaryDir, 0o700); err != nil {
		return fmt.Errorf("competitor binary mkdir: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.cfg.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("competitor request: %w", err)
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("competitor redirect limit exceeded")
		}
		redirect := r.cfg
		redirect.DownloadURL = req.URL.String()
		return validateCompetitorConfig(redirect)
	}}
	response, err := client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("competitor download: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("competitor download: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > r.cfg.MaxDownloadBytes {
		return fmt.Errorf("competitor download exceeds %d bytes", r.cfg.MaxDownloadBytes)
	}
	file, err := os.CreateTemp(r.cfg.BinaryDir, ".lightpanda-download-")
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, os.Remove(file.Name())) }()
	if err := writeVerifiedBinary(ctx, file, response.Body, r.cfg); err != nil {
		return err
	}
	// A hard-link publication is atomic and refuses to replace operator files.
	if err := os.Link(file.Name(), r.BinaryPath()); err != nil {
		return fmt.Errorf("publish verified competitor: %w", err)
	}
	return nil
}

func writeVerifiedBinary(ctx context.Context, file *os.File, body io.Reader, cfg CompetitorConfig) (returnErr error) {
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, cfg.MaxDownloadBytes+1))
	if err != nil {
		return fmt.Errorf("competitor download transfer: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if written == 0 || written > cfg.MaxDownloadBytes {
		return fmt.Errorf("competitor download has invalid length %d", written)
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(actual, cfg.ExpectedSHA256) {
		return fmt.Errorf("competitor checksum mismatch: got %s, want %s", actual, cfg.ExpectedSHA256)
	}
	if err := file.Chmod(0o700); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return nil
}
