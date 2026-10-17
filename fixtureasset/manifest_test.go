package fixtureasset_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func fixtureManifestForTest() (fixtureasset.Manifest, []byte) {
	content := []byte("actual fixture bytes")
	hash := sha256.Sum256(content)
	return fixtureasset.Manifest{
		ID: "fixture-1", OwnerSubsystem: "browser", AssetPath: "fixture:fixture-1", AssetHash: hex.EncodeToString(hash[:]),
		FixtureKind: fixtureasset.KindBenchmark, PrivacyClass: fixtureasset.PrivacyInternal,
		DomainRefs: []string{"domain:one"}, CapabilityRefs: []string{"capability:one"},
		ConnectorRefs: []string{"connector:one"}, ExpectedArtifactRefs: []string{"artifact:one"},
		ReplayMode: fixtureasset.ReplayIsolated, RunnerRef: "runner:renderless", QualityGateRef: "gate:fixtures", Retention: "profile:fixture-evidence",
	}, content
}

func TestFixtureManifestCanonicalRoundTripAndDetachedLists(t *testing.T) {
	manifest, content := fixtureManifestForTest()
	encoded, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := fixtureasset.Decode(encoded, content)
	if err != nil || !reflect.DeepEqual(decoded, manifest) {
		t.Fatalf("actual manifest roundtrip: %v %+v", err, decoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 14 {
		t.Fatalf("incomplete public transport: fields=%d err=%v", len(fields), err)
	}
	detached := manifest.Detached()
	for _, refs := range [][]string{detached.DomainRefs, detached.CapabilityRefs, detached.ConnectorRefs, detached.ExpectedArtifactRefs} {
		refs[0] = "changed"
	}
	for _, refs := range [][]string{manifest.DomainRefs, manifest.CapabilityRefs, manifest.ConnectorRefs, manifest.ExpectedArtifactRefs} {
		if refs[0] == "changed" {
			t.Fatal("manifest reference mutation crossed the copy boundary")
		}
	}
	if _, err := fixtureasset.Decode(encoded, []byte("changed asset")); err == nil {
		t.Fatal("changed actual asset bytes satisfied the old manifest")
	}
}

func TestFixtureManifestRejectsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*fixtureasset.Manifest)
	}{
		{"missing owner", func(m *fixtureasset.Manifest) { m.OwnerSubsystem = "" }},
		{"unknown kind", func(m *fixtureasset.Manifest) { m.FixtureKind = "unknown" }},
		{"readership as content class", func(m *fixtureasset.Manifest) { m.PrivacyClass = "operator" }},
		{"unknown mode", func(m *fixtureasset.Manifest) { m.ReplayMode = "live" }},
		{"missing gate", func(m *fixtureasset.Manifest) { m.QualityGateRef = "" }},
		{"missing retention", func(m *fixtureasset.Manifest) { m.Retention = "" }},
		{"control character", func(m *fixtureasset.Manifest) { m.RunnerRef = "runner\x00" }},
		{"invalid UTF-8", func(m *fixtureasset.Manifest) { m.ID = string([]byte{0xff}) }},
		{"path limit", func(m *fixtureasset.Manifest) { m.AssetPath = strings.Repeat("x", 1025) }},
		{"duplicate ref", func(m *fixtureasset.Manifest) { m.DomainRefs = []string{"a", "a"} }},
		{"empty ref", func(m *fixtureasset.Manifest) { m.DomainRefs = []string{""} }},
		{"reference count", func(m *fixtureasset.Manifest) { m.DomainRefs = make([]string, 65) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, content := fixtureManifestForTest()
			tc.change(&manifest)
			if encoded, err := fixtureasset.Encode(manifest, content); err == nil || encoded != nil {
				t.Fatal("invalid manifest published bytes")
			}
		})
	}
}

func TestFixtureManifestRejectsNoncanonicalJSON(t *testing.T) {
	manifest, content := fixtureManifestForTest()
	encoded, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		string(encoded) + " ", string(encoded) + "{}", "null",
		strings.Replace(string(encoded), "{", `{"unknown":true,`, 1),
		strings.Replace(string(encoded), "{", `{"id":"fixture-1",`, 1),
		strings.Replace(string(encoded), `"domain_refs":["domain:one"]`, `"domain_refs":null`, 1),
		strings.Replace(string(encoded), `"domain_refs":["domain:one"],`, "", 1),
	} {
		if result, err := fixtureasset.Decode([]byte(data), content); err == nil || result.ID != "" {
			t.Fatalf("malformed manifest returned a usable identity: %v", err)
		}
	}
}

func TestFixtureManifestRejectsOversizedActualAssets(t *testing.T) {
	manifest, _ := fixtureManifestForTest()
	for _, size := range []int{fixtureasset.MaxFixtureAssetBytes, fixtureasset.MaxFixtureAssetBytes + 1} {
		content := make([]byte, size)
		hash := sha256.Sum256(content)
		manifest.AssetHash = hex.EncodeToString(hash[:])
		err := manifest.Validate(content)
		if (err != nil) != (size > fixtureasset.MaxFixtureAssetBytes) {
			t.Fatalf("actual byte boundary %d: %v", size, err)
		}
	}
}

func TestFixtureManifestMetadataAndEncodedBudgets(t *testing.T) {
	for _, count := range []int{32, 64} {
		manifest, content := fixtureManifestForTest()
		refs := make([]string, count)
		// Distinct suffixes avoid a duplicate-reference rejection masking the budget.
		for i := range refs {
			refs[i] = strings.Repeat("<", 240) + fmt.Sprintf("%02d", i)
		}
		manifest.DomainRefs, manifest.CapabilityRefs = refs, refs
		manifest.ConnectorRefs, manifest.ExpectedArtifactRefs = refs, refs
		if count == 32 && manifest.Validate(content) != nil {
			t.Fatal("bounded metadata rejected before its encoded expansion")
		}
		if count == 64 && manifest.Validate(content) == nil {
			t.Fatal("aggregate metadata budget exceeded")
		}
		if encoded, err := fixtureasset.Encode(manifest, content); err == nil || encoded != nil {
			t.Fatal("aggregate or encoded expansion exceeded the publication budget")
		}
	}
}

func TestFixtureManifestAcceptsEveryDeclaredClosedValue(t *testing.T) {
	manifest, content := fixtureManifestForTest()
	for _, kind := range []fixtureasset.Kind{fixtureasset.KindBenchmark, fixtureasset.KindCustomerAcceptance, fixtureasset.KindParser, fixtureasset.KindOCR, fixtureasset.KindConnector, fixtureasset.KindUIFlow, fixtureasset.KindReplay, fixtureasset.KindProductionTrace, fixtureasset.KindPolicyScenario, fixtureasset.KindMigration, fixtureasset.KindDoctor, fixtureasset.KindPackValidation} {
		manifest.FixtureKind = kind
		if err := manifest.Validate(content); err != nil {
			t.Fatalf("declared fixture kind %s: %v", kind, err)
		}
	}
	for _, class := range []fixtureasset.PrivacyClass{fixtureasset.PrivacyPublic, fixtureasset.PrivacyInternal, fixtureasset.PrivacyPersonal, fixtureasset.PrivacySpecialCategory} {
		manifest.PrivacyClass = class
		if err := manifest.Validate(content); err != nil {
			t.Fatalf("declared content class %s: %v", class, err)
		}
	}
	for _, mode := range []fixtureasset.ReplayMode{fixtureasset.ReplayIsolated, fixtureasset.ReplayShadow, fixtureasset.ReplayDryRun} {
		manifest.ReplayMode = mode
		if err := manifest.Validate(content); err != nil {
			t.Fatalf("declared replay mode %s: %v", mode, err)
		}
	}
}
