package fixtureasset_test

import (
	"strings"
	"testing"
)

func TestFixtureManifestReferenceIsBoundedInertMetadata(t *testing.T) {
	manifest, content := fixtureManifestForTest()
	if err := manifest.ValidateReference(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(append(content, 'x')); err == nil {
		t.Fatal("metadata admission substituted for actual source-byte admission")
	}
	for _, digest := range []string{"", "0", strings.Repeat("g", 64), strings.Repeat("A", 64)} {
		changed := manifest.Detached()
		changed.AssetHash = digest
		if err := changed.ValidateReference(); err == nil {
			t.Fatal("noncanonical SHA-256 reference admitted")
		}
	}
	manifest.DomainRefs = []string{strings.Repeat("x", 257)}
	if err := manifest.ValidateReference(); err == nil {
		t.Fatal("oversized inert reference admitted")
	}
}
