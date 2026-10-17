package replay

import (
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

// Observation is caller-supplied snapshot data. The producer must obtain it
// from actual execution; this transport does not authenticate observations.
type Observation struct {
	ScenarioID string
	Runner     string
	URL        string
	StatusCode int
	Title      string
	Text       string
	HTML       string
	Duration   time.Duration
}

// CaptureSnapshot uses the existing capture/redaction owner. Identity, digest
// and path are generated; other governance fields must be declared explicitly.
// The source is an observation descriptor, not original executable source code.
func CaptureSnapshot(observed Observation, declaration fixtureasset.Manifest, policy Policy) (fixtureasset.Manifest, []byte, error) {
	if declaration.ID != "" || declaration.AssetHash != "" || declaration.AssetPath != "" {
		return fixtureasset.Manifest{}, nil, fmt.Errorf("replay capture: generated identity/hash/path must not be predeclared")
	}
	source := fixture.Scenario{ID: observed.ScenarioID, Kind: fixture.KindHTML, Description: "Observed inert snapshot", Status: observed.StatusCode}
	result := fixture.CrossResult{URL: observed.URL, StatusCode: observed.StatusCode, Title: observed.Title, Text: observed.Text, HTML: observed.HTML}
	record, err := fixture.CaptureReplay(source, observed.Runner, result, observed.Duration, fixture.SystemMetadata(), redactionConfig(policy), "")
	if err != nil {
		return fixtureasset.Manifest{}, nil, err
	}
	declaration.ID = "replay:" + record.ID
	return record.FixtureAsset(declaration)
}
