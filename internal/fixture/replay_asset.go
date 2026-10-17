package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

const ReplayRunnerRef = "artemis:replay:renderless"

// FixtureAsset exports the exact existing record, not a reconstructed source
// handler. Governance declarations remain explicit and grant no authority.
func (r *Replay) FixtureAsset(manifest fixtureasset.Manifest) (fixtureasset.Manifest, []byte, error) {
	content, err := r.validatedBytes()
	if err != nil {
		return fixtureasset.Manifest{}, nil, err
	}
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	path := "content://sha256/" + digest
	if manifest.ID != "replay:"+r.ID || manifest.FixtureKind != fixtureasset.KindReplay || manifest.RunnerRef != ReplayRunnerRef ||
		manifest.ReplayMode != fixtureasset.ReplayIsolated || manifest.AssetHash != "" && manifest.AssetHash != digest || manifest.AssetPath != "" && manifest.AssetPath != path {
		return fixtureasset.Manifest{}, nil, fmt.Errorf("replay asset: capture and declared snapshot input differ")
	}
	manifest.AssetHash, manifest.AssetPath = digest, path
	if err := manifest.Validate(content); err != nil {
		return fixtureasset.Manifest{}, nil, err
	}
	return manifest.Detached(), content, nil
}
