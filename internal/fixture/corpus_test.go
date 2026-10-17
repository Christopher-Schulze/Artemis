package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
	"github.com/Christopher-Schulze/Artemis/js"
)

func TestCorpusSourcePinIncludesCSSImplementationTestsAndBindings(t *testing.T) {
	cssHash, err := css.SourceHash()
	if err != nil {
		t.Fatal(err)
	}
	jsHash, err := js.SourceHash()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := sourcepin.Hash(corpusSources, cssHash, jsHash)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := corpusSourceHash()
	if err != nil || actual != expected {
		t.Fatalf("corpus omitted CSS implementation/test closure: %q %q %v", actual, expected, err)
	}
	omitted, err := sourcepin.Hash(corpusSources)
	if err != nil || omitted == actual {
		t.Fatalf("missing CSS closure did not invalidate corpus identity: %q %v", omitted, err)
	}
}

func TestCorpusSourcePinIncludesEveryCurrentProducer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") || !strings.HasSuffix(name, ".go") || !(strings.HasPrefix(name, "scenarios_") || strings.HasPrefix(name, "corpus") || name == "server.go" || name == "scenario.go" || name == "validation.go") {
			continue
		}
		embedded, err := corpusSources.ReadFile(name)
		if err != nil {
			t.Fatalf("production corpus owner %s is not pinned: %v", name, err)
		}
		actual, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(actual, embedded) {
			t.Fatalf("compiled source pin differs from %s: %v", name, err)
		}
	}
}

func TestCorpusAssetsBindEveryCaseAndRunnerToOnePayload(t *testing.T) {
	var common []byte
	identities := make(map[string]bool)
	for _, runner := range []string{"renderless", "chromium", "hybrid", "serve", "agent", "artemis"} {
		manifests, data, err := CorpusAssets(runner)
		if err != nil || len(manifests) != len(DefaultScenarios()) {
			t.Fatalf("complete %s corpus: %d %v", runner, len(manifests), err)
		}
		if common != nil && !bytes.Equal(common, data) {
			t.Fatal("runner selection duplicated or changed the shared source payload")
		}
		common = data
		for _, manifest := range manifests {
			if identities[manifest.ID] {
				t.Fatal("case/runner identities collide")
			}
			identities[manifest.ID] = true
			if err := ValidateCorpusAsset(manifest, data); err != nil {
				t.Fatalf("actual complete corpus manifest: %v", err)
			}
		}
	}
	if manifests, data, err := CorpusAssets("unknown"); err == nil || manifests != nil || data != nil {
		t.Fatal("unknown runner acquired corpus assets")
	}
	t.Logf("actual corpus: %d scenarios, %d case/runner manifests, %d shared payload bytes", len(DefaultScenarios()), len(identities), len(common))
}

func TestCorpusRejectsResealedProgramRouteAndAssertionMutations(t *testing.T) {
	manifests, original, err := CorpusAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*corpusInput)
	}{
		{"schema", func(input *corpusInput) { input.SchemaVersion++ }},
		{"source pin", func(input *corpusInput) { input.SourceSHA256 = strings.Repeat("0", 64) }},
		{"Go runtime", func(input *corpusInput) { input.GoVersion = "other" }},
		{"omitted case", func(input *corpusInput) { input.Cases = input.Cases[1:] }},
		{"duplicate case", func(input *corpusInput) { input.Cases = append(input.Cases, input.Cases[0]) }},
		{"route", func(input *corpusInput) { input.Cases[0].Scenario.Path = "/different" }},
		{"assertion", func(input *corpusInput) { input.Cases[0].Scenario.Expect.Contains = nil }},
		{"handler omission", func(input *corpusInput) {
			for index := range input.Cases {
				if input.Cases[index].HasHandler {
					input.Cases[index].HasHandler = false
					return
				}
			}
			t.Fatal("dynamic handler control missing")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input corpusInput
			if err := json.Unmarshal(original, &input); err != nil {
				t.Fatal(err)
			}
			test.edit(&input)
			changed, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			manifest := manifests[0].Detached()
			manifest.AssetHash = fmt.Sprintf("%x", sha256.Sum256(changed))
			manifest.AssetPath = "content://sha256/" + manifest.AssetHash
			if err := manifest.Validate(changed); err != nil {
				t.Fatalf("resealed control must pass digest admission: %v", err)
			}
			if err := ValidateCorpusAsset(manifest, changed); err == nil {
				t.Fatal("resealed source/assertion mutation admitted")
			}
			if server, err := NewServerFromCorpus(changed); err == nil || server != nil {
				if server != nil {
					server.Close()
				}
				t.Fatal("changed corpus acquired a live server")
			}
		})
	}
}

func TestCorpusServerRetainsActualDynamicBehaviorAndRejectsMutation(t *testing.T) {
	data, err := DefaultCorpusBytes()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerFromCorpus(data)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	data[0] ^= 1
	scenarios, err := server.CorpusScenarios()
	if err != nil || len(scenarios) != len(DefaultScenarios()) {
		t.Fatalf("caller bytes changed the admitted server: %v", err)
	}
	manifest, err := server.CorpusManifest("renderless", scenarios[0])
	if err != nil || manifest.AssetHash == fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("server manifest lost its owned source: %v", err)
	}
	changed := cloneScenario(scenarios[0])
	changed.Expect.Contains = nil
	if _, err := server.CorpusManifest("renderless", changed); err == nil {
		t.Fatal("weakened requested assertions acquired a manifest")
	}
	response, err := http.PostForm(server.URL("/form-submit"), url.Values{"name": {"Actual input"}, "email": {"actual@fixture.test"}})
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err := response.Body.Close(); err != nil || readErr != nil || response.StatusCode != 200 || !bytes.Contains(body, []byte("Actual input")) || !bytes.Contains(body, []byte("actual@fixture.test")) {
		t.Fatalf("dynamic POST handler was replaced by serialized/static output: %s %v %v", body, readErr, err)
	}
	if err := server.RegisterHTML("/extra", "extra"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.CorpusManifest("renderless", scenarios[0]); err == nil {
		t.Fatal("expanded route set retained the immutable corpus claim")
	}
	server.Close()
	if _, err := server.CorpusScenarios(); err == nil {
		t.Fatal("closed corpus owner exposed executable inputs")
	}
}
