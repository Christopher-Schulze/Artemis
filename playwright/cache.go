package playwright

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	lastKnownGoodFile = "last-known-good.json"
	artifactsDir      = "artifacts"
	// DefaultMinFreeBytes is the headroom kept free beyond an artifact's size.
	DefaultMinFreeBytes int64 = 1 << 30
)

// LastKnownGood is the durable pointer to the newest admitted artifact.
type LastKnownGood struct {
	ArtifactDigest string    `json:"artifact_digest"`
	AdmittedAt     time.Time `json:"admitted_at"`
	KeyID          string    `json:"key_id"`
}

// Cache is the shared, content-addressed Playwright artifact cache. Each
// admitted artifact lives under artifacts/<digest>; last-known-good.json
// names the artifact the lane serves. A failed candidate never replaces it.
type Cache struct {
	dir     string
	trusted TrustedKeys
	minFree int64
	now     func() time.Time
}

// OpenCache creates the cache directory and validates its configuration.
func OpenCache(dir string, trusted TrustedKeys, minFree int64) (*Cache, error) {
	if strings.TrimSpace(dir) == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("playwright cache: absolute directory is required")
	}
	if len(trusted) == 0 {
		return nil, errors.New("playwright cache: at least one trusted signer key is required")
	}
	if minFree <= 0 {
		minFree = DefaultMinFreeBytes
	}
	if err := os.MkdirAll(filepath.Join(dir, artifactsDir), 0o700); err != nil {
		return nil, fmt.Errorf("playwright cache: %w", err)
	}
	return &Cache{dir: dir, trusted: trusted, minFree: minFree, now: time.Now}, nil
}

// Status is the verified state of the cache's last-known-good artifact.
type Status struct {
	Admission   Admission
	Admitted    bool
	Diagnostics []string
	Remediation string
}

// Load re-verifies the last-known-good artifact from the cache alone, so an
// offline restart serves exactly the bytes admitted before. A missing or
// failed artifact is reported, never deleted or substituted.
func (c *Cache) Load(ctx context.Context) Status {
	pointer, err := c.lastKnownGood()
	if errors.Is(err, fs.ErrNotExist) {
		return Status{Diagnostics: []string{"no Playwright artifact has been provisioned"}, Remediation: "install a signed Playwright artifact into the cache"}
	}
	if err != nil {
		return Status{Diagnostics: []string{"last-known-good pointer unreadable: " + err.Error()}, Remediation: "reinstall a signed Playwright artifact"}
	}
	admission, err := Admit(ctx, c.artifactDir(pointer.ArtifactDigest), c.trusted)
	if err != nil {
		return Status{Diagnostics: []string{"cached artifact failed verification: " + err.Error()}, Remediation: "reinstall a signed Playwright artifact"}
	}
	if admission.ArtifactDigest != pointer.ArtifactDigest {
		return Status{Diagnostics: []string{"cached artifact digest does not match its last-known-good pointer"}, Remediation: "reinstall a signed Playwright artifact"}
	}
	return Status{Admission: admission, Admitted: true}
}

// Install admits candidateDir, copies it into the cache, re-admits the copy
// and only then moves the last-known-good pointer. Any failure leaves the
// previous last-known-good artifact serving.
func (c *Cache) Install(ctx context.Context, candidateDir string) (Admission, error) {
	candidate, err := Admit(ctx, candidateDir, c.trusted)
	if err != nil {
		return Admission{}, err
	}
	free, err := availableBytes(c.dir)
	if err != nil {
		return Admission{}, fmt.Errorf("%w: %v", ErrInsufficientSpace, err)
	}
	if need := candidate.Manifest.totalBytes() + c.minFree; free < need {
		return Admission{}, fmt.Errorf("%w: %d bytes free, %d required", ErrInsufficientSpace, free, need)
	}
	target := c.artifactDir(candidate.ArtifactDigest)
	if _, statErr := os.Stat(target); errors.Is(statErr, fs.ErrNotExist) {
		staging, err := os.MkdirTemp(filepath.Join(c.dir, artifactsDir), ".staging-")
		if err != nil {
			return Admission{}, err
		}
		if err := copyArtifact(ctx, candidateDir, staging, candidate.Manifest); err != nil {
			return Admission{}, errors.Join(err, os.RemoveAll(staging))
		}
		if err := os.Rename(staging, target); err != nil {
			return Admission{}, errors.Join(err, os.RemoveAll(staging))
		}
	}
	installed, err := Admit(ctx, target, c.trusted)
	if err != nil {
		return Admission{}, fmt.Errorf("playwright cache: installed copy failed verification: %w", err)
	}
	if err := c.writeLastKnownGood(LastKnownGood{ArtifactDigest: installed.ArtifactDigest, AdmittedAt: c.now().UTC(), KeyID: installed.KeyID}); err != nil {
		return Admission{}, err
	}
	return installed, nil
}

func (c *Cache) artifactDir(digest string) string {
	return filepath.Join(c.dir, artifactsDir, strings.TrimPrefix(digest, "sha256:"))
}

func (c *Cache) lastKnownGood() (LastKnownGood, error) {
	var pointer LastKnownGood
	data, err := os.ReadFile(filepath.Join(c.dir, lastKnownGoodFile))
	if err != nil {
		return pointer, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pointer); err != nil {
		return pointer, err
	}
	if !strings.HasPrefix(pointer.ArtifactDigest, "sha256:") || len(pointer.ArtifactDigest) != len("sha256:")+64 {
		return pointer, fmt.Errorf("invalid artifact digest %q", pointer.ArtifactDigest)
	}
	return pointer, nil
}

func (c *Cache) writeLastKnownGood(pointer LastKnownGood) error {
	data, err := json.Marshal(pointer)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(c.dir, ".lkg-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(data); err != nil {
		return errors.Join(err, temp.Close(), os.Remove(name))
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(err, temp.Close(), os.Remove(name))
	}
	if err := temp.Close(); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	return os.Rename(name, filepath.Join(c.dir, lastKnownGoodFile))
}

// copyArtifact copies exactly the signed envelope and listed files.
func copyArtifact(ctx context.Context, from, to string, manifest Manifest) error {
	names := []string{ManifestFile, SignatureFile}
	for _, file := range manifest.Files {
		names = append(names, file.Path)
	}
	source, err := os.OpenRoot(from)
	if err != nil {
		return err
	}
	defer source.Close()
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := copyFile(source, name, filepath.Join(to, filepath.FromSlash(name))); err != nil {
			return fmt.Errorf("copy %q: %w", name, err)
		}
	}
	return nil
}

func copyFile(source *os.Root, name, target string) error {
	in, err := source.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(err, out.Close())
	}
	return errors.Join(out.Sync(), out.Close())
}
