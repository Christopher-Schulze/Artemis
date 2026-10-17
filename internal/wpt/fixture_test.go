package wpt

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func TestWPTFixtureBindsCompleteMirrorAndEveryPinnedAssertion(t *testing.T) {
	manifest, content, err := DefaultFixture()
	if err != nil {
		t.Fatal(err)
	}
	input, err := decodeFixture(manifest, content)
	if err != nil || len(input.Subset.Tests) != len(DefaultSubset().Tests) || len(input.Files) == 0 {
		t.Fatalf("complete pinned fixture admission: %+v %v", input.Subset, err)
	}
	metadata, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixtureasset.Decode(metadata, content); err != nil {
		t.Fatal(err)
	}
	for _, file := range input.Files {
		actual, err := testdata.ReadFile("testdata/wpt/" + file.Path)
		if err != nil || string(actual) != string(file.Content) {
			t.Fatalf("exact admitted mirror file %s: %v", file.Path, err)
		}
	}
	for _, capabilities := range [][]string{nil, {"invented"}} {
		changed := manifest.Detached()
		changed.CapabilityRefs = capabilities
		if err := ValidateFixture(t.Context(), changed, content); err == nil {
			t.Fatal("manifest omitted or forged the actual pinned capability mapping")
		}
	}
}

func TestWPTFixtureRejectsResealedSourceAndAssertionChanges(t *testing.T) {
	manifest, content, err := DefaultFixture()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*fixtureInput)
	}{
		{"version", func(input *fixtureInput) { input.Version++ }},
		{"revision", func(input *fixtureInput) { input.Subset.Revision = "different" }},
		{"origin", func(input *fixtureInput) { input.Subset.OriginURL = "https://other.invalid" }},
		{"expectation", func(input *fixtureInput) { input.Subset.Tests[0].Expected = OutcomeSkip }},
		{"capability", func(input *fixtureInput) { input.Subset.Tests[0].CapabilityID = "other" }},
		{"test identity", func(input *fixtureInput) { input.Subset.Tests[0].Name = "other" }},
		{"omitted file", func(input *fixtureInput) { input.Files = input.Files[1:] }},
		{"duplicate file", func(input *fixtureInput) { input.Files = append(input.Files, input.Files[0]) }},
		{"path alias", func(input *fixtureInput) { input.Files[0].Path = "../other.html" }},
		{"modified script", func(input *fixtureInput) {
			for index := range input.Files {
				if input.Files[index].Path == "resources/testharness.js" {
					input.Files[index].Content = append(input.Files[index].Content, []byte(";window.__wptResults={};")...)
					return
				}
			}
			t.Fatal("actual harness source control missing")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input fixtureInput
			if err := json.Unmarshal(content, &input); err != nil {
				t.Fatal(err)
			}
			test.edit(&input)
			changed, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			bound := manifest.Detached()
			bound.AssetHash = fmt.Sprintf("%x", sha256.Sum256(changed))
			bound.AssetPath = "content://sha256/" + bound.AssetHash
			if err := bound.Validate(changed); err != nil {
				t.Fatalf("resealed control must pass transport digest admission: %v", err)
			}
			if runner, err := NewRunnerFromFixture(t.Context(), bound, changed); err == nil || runner != nil {
				if runner != nil {
					if closeErr := runner.Close(); closeErr != nil {
						t.Error(closeErr)
					}
				}
				t.Fatal("resealed source or weaker assertion acquired a runner")
			}
		})
	}
}

func TestWPTFixtureRejectsChangedDeclaredSubsetAndClosedOwner(t *testing.T) {
	runner, err := NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, field := range []string{"expectation", "capability"} {
		subset := DefaultSubset()
		if field == "expectation" {
			subset.Tests[0].Expected = OutcomeSkip
		} else {
			subset.Tests[0].CapabilityID = "other"
		}
		if results, err := runner.Run(t.Context(), subset); err == nil || len(results) != 0 {
			t.Fatal("weakened declared subset reached execution")
		}
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), DefaultSubset()); err == nil {
		t.Fatal("closed fixture owner remained executable")
	}
}
