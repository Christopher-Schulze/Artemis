package benchmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// ValidateArtifactBundle admits an inert complete bundle without filesystem
// copies. Hashes bind exact bytes; they do not authenticate an execution.
func ValidateArtifactBundle(bundle ArtifactBundle) error {
	if len(bundle.Manifest) == 0 || int64(len(bundle.Manifest)) > maxArtifactManifestBytes {
		return fmt.Errorf("artifact bundle: bounded manifest required")
	}
	var receipt ArtifactReceipt
	if err := decodeCohortReceipt(bundle.Manifest, &receipt); err != nil {
		return err
	}
	declared, err := json.MarshalIndent(bundle.Receipt, "", "  ")
	if err != nil || !bytes.Equal(declared, bundle.Manifest) || len(bundle.Files) != len(receipt.Artifacts) {
		return fmt.Errorf("artifact bundle: exact complete receipt required")
	}
	for index, expected := range receipt.Artifacts {
		file := bundle.Files[index]
		if file.Digest != expected || int64(len(file.Content)) != expected.Bytes || fmt.Sprintf("%x", sha256.Sum256(file.Content)) != expected.SHA256 {
			return fmt.Errorf("artifact bundle: member mismatch: %s", expected.Path)
		}
	}
	return nil
}
