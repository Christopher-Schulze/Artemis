package wpt

import (
	"bytes"
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	internalwpt "github.com/Christopher-Schulze/Artemis/internal/wpt"
)

// ValidateResults checks the complete pinned report identity and closed status
// vocabulary. It grants neither producer authenticity nor passing conformance.
func ValidateResults(ctx context.Context, manifest fixtureasset.Manifest, content []byte, results []Result) error {
	if err := ValidateFixture(ctx, manifest, content); err != nil {
		return err
	}
	bound, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		return err
	}
	tests := internalwpt.DefaultSubset().Tests
	if len(results) != len(tests) {
		return fmt.Errorf("wpt: complete ordered pinned reports required")
	}
	for index, result := range results {
		if result.Path != tests[index].Path || result.Name != tests[index].Name || !utf8.ValidString(result.Message) || len(result.Message) > 16384 {
			return fmt.Errorf("wpt: changed, reordered or oversized report identity")
		}
		if err := (internalwpt.Result{Path: result.Path, Name: result.Name, Status: result.Status, HarnessStatus: result.HarnessStatus}).Validate(); err != nil {
			return err
		}
		actual, err := fixtureasset.Encode(result.FixtureManifest, content)
		if err != nil || !bytes.Equal(actual, bound) {
			return fmt.Errorf("wpt: report manifest differs from admitted fixture")
		}
	}
	return nil
}
