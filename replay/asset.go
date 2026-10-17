// Package replay admits existing captured records and runs inert snapshots.
// Storage, identity permissions and retention remain with the embedding owner.
package replay

import (
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

const RunnerRef = fixture.ReplayRunnerRef

// Policy selects the existing redaction profile. The zero value requires the
// default redacted profile; unredacted records need an explicit opt-out.
type Policy struct {
	AllowUnredacted bool
	Mask            string
	Patterns        []string
}

// BindAsset validates existing canonical record bytes and binds an explicit
// governance declaration. It authenticates neither the producer nor its claims.
func BindAsset(content []byte, manifest fixtureasset.Manifest, policy Policy) (fixtureasset.Manifest, error) {
	record, err := fixture.DecodeReplay(content, redactionConfig(policy))
	if err != nil {
		return fixtureasset.Manifest{}, err
	}
	bound, _, err := record.FixtureAsset(manifest)
	return bound, err
}

// ValidateAsset requires the complete byte-bound declaration as well as the
// existing strict schema, capture checksum and matching redaction profile.
func ValidateAsset(content []byte, manifest fixtureasset.Manifest, policy Policy) error {
	_, err := admitAsset(content, manifest, policy)
	return err
}

func admitAsset(content []byte, manifest fixtureasset.Manifest, policy Policy) (*fixture.Replay, error) {
	if err := manifest.Validate(content); err != nil {
		return nil, err
	}
	record, err := fixture.DecodeReplay(content, redactionConfig(policy))
	if err != nil {
		return nil, err
	}
	if _, _, err := record.FixtureAsset(manifest); err != nil {
		return nil, err
	}
	return record, nil
}

func redactionConfig(policy Policy) fixture.RedactionConfig {
	return fixture.RedactionConfig{Enabled: !policy.AllowUnredacted, Mask: policy.Mask, Patterns: policy.Patterns}
}
