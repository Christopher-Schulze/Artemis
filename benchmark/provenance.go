package benchmark

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// RunProvenance identifies the executed Artemis code and declared cache policy.
type RunProvenance struct {
	ArtemisSHA256     string `json:"artemisSHA256"`
	SourceRevision    string `json:"sourceRevision"`
	SourceModified    bool   `json:"sourceModified"`
	SourceTreeState   string `json:"sourceTreeState"`
	SamplePolicy      string `json:"samplePolicy"`
	OSCacheState      string `json:"osCacheState"`
	JSContextPoolSize int    `json:"jsContextPoolSize"`
	WarmPrimes        int    `json:"warmPrimes"`
	GOMAXPROCS        int    `json:"gomaxprocs"`
}

// ScenarioEvidence binds semantic requirements to the actual fixture bytes.
type ScenarioEvidence struct {
	ID                 string                 `json:"id"`
	HTMLSHA256         string                 `json:"htmlSHA256"`
	ScriptCount        int                    `json:"scriptCount"`
	ExpectedTitle      string                 `json:"expectedTitle"`
	ExpectedLinks      int                    `json:"expectedLinks"`
	ExpectedParagraphs int                    `json:"expectedParagraphs"`
	FixtureManifest    *fixtureasset.Manifest `json:"fixtureManifest,omitempty"`
}

// ClaimEvidence limits performance wording to the recorded workload and proof.
type ClaimEvidence struct {
	Status    string    `json:"status"`
	Scope     []string  `json:"scope"`
	ProofRefs []string  `json:"proofRefs"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func currentProvenance() (RunProvenance, error) {
	result := RunProvenance{SourceRevision: "unavailable", SourceTreeState: "unavailable", SamplePolicy: SamplePolicy,
		OSCacheState: "uncontrolled", JSContextPoolSize: contextPoolSize, WarmPrimes: 1, GOMAXPROCS: runtime.GOMAXPROCS(0)}
	path, err := os.Executable()
	if err != nil {
		return result, err
	}
	result.ArtemisSHA256, err = fileSHA256(path)
	if err != nil {
		return result, err
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				result.SourceRevision = setting.Value
			}
			if setting.Key == "vcs.modified" {
				result.SourceModified = setting.Value == "true"
				result.SourceTreeState = "clean"
				if result.SourceModified {
					result.SourceTreeState = "modified"
				}
			}
		}
	}
	return result, nil
}

func scenarioEvidence(scenarios []Scenario) []ScenarioEvidence {
	result := make([]ScenarioEvidence, 0, len(scenarios))
	for _, scenario := range scenarios {
		result = append(result, ScenarioEvidence{ID: scenario.ID, HTMLSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(scenario.HTML))),
			ScriptCount: scenario.ScriptCount, ExpectedTitle: scenario.ExpectTitle, ExpectedLinks: scenario.ExpectLinks, ExpectedParagraphs: scenario.ExpectParagraphs})
	}
	return result
}

func downloadOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
	return parsed.String()
}

func (s *Scorecard) claimEvidence() ClaimEvidence {
	result := ClaimEvidence{Status: "invalid", ProofRefs: []string{"scorecard.json#/samples", "scorecard.json#/priming", "scorecard.json#/provenance", "scorecard.json#/scenarios", "manifest.json#/artifacts"}, ExpiresAt: s.Date.Add(7 * 24 * time.Hour)}
	for _, workload := range s.Workloads {
		result.Scope = append(result.Scope, workload.Identity())
	}
	if s.Honest {
		result.Status = "observed"
		if s.Mode == ModeHeadToHead {
			result.Status = "benchmarked"
		}
	}
	return result
}
