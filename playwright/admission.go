package playwright

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

var (
	// ErrSignatureInvalid reports a missing, malformed or untrusted signature.
	ErrSignatureInvalid = errors.New("playwright artifact signature invalid")
	// ErrArtifactCorrupt reports bytes that differ from the signed manifest.
	ErrArtifactCorrupt = errors.New("playwright artifact corrupt")
	// ErrABIIncompatible reports an artifact built for another session ABI.
	ErrABIIncompatible = errors.New("playwright artifact session ABI incompatible")
	// ErrInsufficientSpace reports a cache volume without the required headroom.
	ErrInsufficientSpace = errors.New("playwright cache volume lacks free space")
)

// TrustedKeys maps a signer key ID (KeyID) to its Ed25519 public key.
type TrustedKeys map[string]ed25519.PublicKey

// KeyID is the stable identifier of an Ed25519 signer key.
func KeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:8])
}

// Signature is the detached signature envelope next to the manifest.
type Signature struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// Admission is one fully verified artifact.
type Admission struct {
	Dir            string
	ArtifactDigest string
	KeyID          string
	Manifest       Manifest
	// ScriptCompiler reports whether the compiler and context declarations
	// required by browser_script are present.
	ScriptCompiler bool
}

// Admit verifies the artifact rooted at dir: the manifest signature against a
// trusted key, the session ABI, every listed file's role, size and SHA-256,
// and the absence of unlisted files. Files are opened through an os.Root, so
// a symlink cannot escape the artifact.
func Admit(ctx context.Context, dir string, trusted TrustedKeys) (Admission, error) {
	if ctx == nil {
		return Admission{}, errors.New("playwright admission: context is required")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Admission{}, fmt.Errorf("%w: open artifact: %v", ErrArtifactCorrupt, err)
	}
	defer root.Close()
	manifestBytes, err := readBounded(root, ManifestFile, maxManifestBytes)
	if err != nil {
		return Admission{}, fmt.Errorf("%w: read manifest: %v", ErrArtifactCorrupt, err)
	}
	keyID, err := verifySignature(root, manifestBytes, trusted)
	if err != nil {
		return Admission{}, err
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil {
		return Admission{}, err
	}
	if manifest.SessionABIVersion != SessionABIVersion {
		return Admission{}, fmt.Errorf("%w: artifact %q, Artemis %q", ErrABIIncompatible, manifest.SessionABIVersion, SessionABIVersion)
	}
	if err := verifyFiles(ctx, root, manifest); err != nil {
		return Admission{}, err
	}
	sum := sha256.Sum256(manifestBytes)
	return Admission{
		Dir: dir, ArtifactDigest: "sha256:" + hex.EncodeToString(sum[:]), KeyID: keyID, Manifest: manifest,
		ScriptCompiler: manifest.hasRole(RoleCompiler) && manifest.hasRole(RoleDeclarations),
	}, nil
}

func verifySignature(root *os.Root, manifest []byte, trusted TrustedKeys) (string, error) {
	raw, err := readBounded(root, SignatureFile, 4096)
	if err != nil {
		return "", fmt.Errorf("%w: read signature: %v", ErrSignatureInvalid, err)
	}
	var envelope Signature
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return "", fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	public, ok := trusted[envelope.KeyID]
	if !ok || len(public) != ed25519.PublicKeySize || KeyID(public) != envelope.KeyID {
		return "", fmt.Errorf("%w: signer %q is not trusted", ErrSignatureInvalid, envelope.KeyID)
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(public, manifest, signature) {
		return "", fmt.Errorf("%w: signature does not verify", ErrSignatureInvalid)
	}
	return envelope.KeyID, nil
}

func verifyFiles(ctx context.Context, root *os.Root, manifest Manifest) error {
	listed := make(map[string]File, len(manifest.Files))
	for _, file := range manifest.Files {
		listed[file.Path] = file
	}
	seen := 0
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || name == ManifestFile || name == SignatureFile {
			return nil
		}
		file, ok := listed[name]
		if !ok || !entry.Type().IsRegular() {
			return fmt.Errorf("%w: unlisted or non-regular entry %q", ErrArtifactCorrupt, name)
		}
		if err := verifyFile(root, file); err != nil {
			return err
		}
		seen++
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrArtifactCorrupt) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrArtifactCorrupt, err)
	}
	if seen != len(listed) {
		return fmt.Errorf("%w: %d of %d listed files present", ErrArtifactCorrupt, seen, len(listed))
	}
	return nil
}

func verifyFile(root *os.Root, file File) error {
	handle, err := root.Open(file.Path)
	if err != nil {
		return fmt.Errorf("%w: open %q: %v", ErrArtifactCorrupt, file.Path, err)
	}
	defer handle.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(handle, file.Size+1))
	if err != nil {
		return fmt.Errorf("%w: read %q: %v", ErrArtifactCorrupt, file.Path, err)
	}
	if written != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("%w: %q does not match its signed digest", ErrArtifactCorrupt, file.Path)
	}
	return nil
}

func readBounded(root *os.Root, name string, limit int64) ([]byte, error) {
	handle, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return data, nil
}
