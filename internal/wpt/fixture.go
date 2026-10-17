package wpt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

const FixtureRunnerRef = "artemis:wpt:renderless"

// ValidateFixture applies the same complete input admission without resources.
func ValidateFixture(ctx context.Context, manifest fixtureasset.Manifest, content []byte) error {
	if ctx == nil {
		return fmt.Errorf("wpt fixture: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := decodeFixture(manifest, content); err != nil {
		return err
	}
	return ctx.Err()
}

type fixtureInput struct {
	Version int           `json:"version"`
	Subset  Subset        `json:"subset"`
	Files   []fixtureFile `json:"files"`
}

type fixtureFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

// DefaultFixture binds the complete pinned subset and mirror, including the
// unchanged harness resources and every declared semantic expectation.
func DefaultFixture() (fixtureasset.Manifest, []byte, error) {
	input, content, err := embeddedFixtureInput()
	if err != nil {
		return fixtureasset.Manifest{}, nil, err
	}
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	manifest := fixtureasset.Manifest{ID: "wpt:" + DefaultSubset().Revision, OwnerSubsystem: "artemis-wpt",
		AssetPath: "content://sha256/" + digest, AssetHash: digest, FixtureKind: fixtureasset.KindBenchmark,
		PrivacyClass: fixtureasset.PrivacyPublic, CapabilityRefs: fixtureCapabilities(input.Subset), ReplayMode: fixtureasset.ReplayIsolated,
		RunnerRef: FixtureRunnerRef, QualityGateRef: "go-test:internal/wpt:TestRunner", Retention: "wpt:run-evidence"}
	if err := manifest.Validate(content); err != nil {
		return fixtureasset.Manifest{}, nil, err
	}
	return manifest.Detached(), content, nil
}

func embeddedFixtureInput() (fixtureInput, []byte, error) {
	input := fixtureInput{Version: 1, Subset: DefaultSubset()}
	var total int
	err := fs.WalkDir(testdata, "testdata/wpt", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if len(input.Files) >= 1024 {
			return fmt.Errorf("wpt fixture: embedded file count exceeds limit")
		}
		content, err := testdata.ReadFile(path)
		if err != nil {
			return err
		}
		if len(content) > 8<<20-total {
			return fmt.Errorf("wpt fixture: embedded file exceeds byte limit")
		}
		total += len(content)
		input.Files = append(input.Files, fixtureFile{Path: strings.TrimPrefix(path, "testdata/wpt/"), Content: content})
		return nil
	})
	if err != nil {
		return fixtureInput{}, nil, err
	}
	content, err := json.Marshal(input)
	if err != nil {
		return fixtureInput{}, nil, err
	}
	if len(content) > fixtureasset.MaxFixtureAssetBytes {
		return fixtureInput{}, nil, fmt.Errorf("wpt fixture: encoded input exceeds byte limit")
	}
	return input, content, nil
}

func decodeFixture(manifest fixtureasset.Manifest, content []byte) (fixtureInput, error) {
	if err := manifest.Validate(content); err != nil {
		return fixtureInput{}, err
	}
	if manifest.ID != "wpt:"+DefaultSubset().Revision || manifest.FixtureKind != fixtureasset.KindBenchmark ||
		manifest.RunnerRef != FixtureRunnerRef || manifest.ReplayMode != fixtureasset.ReplayIsolated ||
		manifest.AssetPath != "content://sha256/"+manifest.AssetHash {
		return fixtureInput{}, fmt.Errorf("wpt fixture: pinned identity, runner and mode required")
	}
	_, pinned, err := embeddedFixtureInput()
	if err != nil {
		return fixtureInput{}, err
	}
	if !bytes.Equal(content, pinned) {
		return fixtureInput{}, fmt.Errorf("wpt fixture: complete pinned source and assertions differ")
	}
	var input fixtureInput
	if err := json.Unmarshal(content, &input); err != nil {
		return fixtureInput{}, err
	}
	if err := validateFixtureCapabilities(manifest.CapabilityRefs, input.Subset); err != nil {
		return fixtureInput{}, err
	}
	return input, nil
}

func fixtureCapabilities(subset Subset) []string {
	seen := make(map[string]bool)
	for _, test := range subset.Tests {
		seen[test.CapabilityID] = true
	}
	capabilities := make([]string, 0, len(seen))
	for capability := range seen {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return capabilities
}

func validateFixtureCapabilities(declared []string, subset Subset) error {
	expected := fixtureCapabilities(subset)
	if len(declared) != len(expected) {
		return fmt.Errorf("wpt fixture: capability references differ from pinned tests")
	}
	for _, capability := range expected {
		found := false
		for _, ref := range declared {
			if capability == ref {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("wpt fixture: capability references differ from pinned tests")
		}
	}
	return nil
}
