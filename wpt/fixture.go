// Package wpt exposes the existing pinned conformance fixture and runner.
// Admission grants neither producer authenticity nor passing conformance.
package wpt

import (
	"context"
	"errors"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	internalwpt "github.com/Christopher-Schulze/Artemis/internal/wpt"
)

// Result carries actual completed harness evidence bound to admitted bytes.
type Result struct {
	Path            string                `json:"path"`
	Name            string                `json:"name"`
	Status          string                `json:"status"`
	Message         string                `json:"message"`
	HarnessStatus   string                `json:"harness_status"`
	FixtureManifest fixtureasset.Manifest `json:"fixture_manifest"`
}

// DefaultFixture returns the existing pinned assertions and every unchanged
// embedded file as a portable canonical asset, without a filesystem copy.
func DefaultFixture() (fixtureasset.Manifest, []byte, error) {
	return internalwpt.DefaultFixture()
}

// ValidateFixture admits data without creating a server or JavaScript engine.
func ValidateFixture(ctx context.Context, manifest fixtureasset.Manifest, content []byte) error {
	return internalwpt.ValidateFixture(ctx, manifest, content)
}

// RunFixture uses the existing runner and exact pinned expectations. The host
// must resolve current permission/scanner/retention before calling this API.
func RunFixture(ctx context.Context, manifest fixtureasset.Manifest, content []byte) (results []Result, returnErr error) {
	runner, err := internalwpt.NewRunnerFromFixture(ctx, manifest, content)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := errors.Join(runner.Close(), ctx.Err()); err != nil {
			results = nil
			returnErr = errors.Join(returnErr, err)
		}
	}()
	observed, err := runner.Run(ctx, internalwpt.DefaultSubset())
	if err != nil {
		return nil, err
	}
	if len(observed) != len(internalwpt.DefaultSubset().Tests) {
		return nil, fmt.Errorf("wpt: incomplete pinned result set")
	}
	results, err = projectResults(observed)
	if validationErr := ValidateResults(ctx, manifest, content, results); validationErr != nil {
		return nil, errors.Join(err, validationErr)
	}
	return results, err
}

func projectResults(observed []internalwpt.Result) ([]Result, error) {
	results := make([]Result, 0, len(observed))
	var failures []error
	for _, result := range observed {
		if result.FixtureManifest == nil {
			return nil, fmt.Errorf("wpt: completed report has no admitted fixture identity")
		}
		results = append(results, Result{Path: result.Path, Name: result.Name, Status: result.Status,
			Message: result.Message, HarnessStatus: result.HarnessStatus, FixtureManifest: result.FixtureManifest.Detached()})
		if result.Status != "PASS" || result.HarnessStatus != "OK" {
			failures = append(failures, fmt.Errorf("wpt: conformance not proven for %s/%s: status=%s harness=%s message=%q", result.Path, result.Name, result.Status, result.HarnessStatus, result.Message))
		}
	}
	return results, errors.Join(failures...)
}
