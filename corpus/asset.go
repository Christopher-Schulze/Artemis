// Package corpus exposes the existing pinned Artemis fixture inputs.
// It owns no registry, durable store, permission or passing-coverage authority.
package corpus

import (
	"context"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

// DefaultAssets returns per-case manifests with one shared complete payload.
func DefaultAssets(runner string) ([]fixtureasset.Manifest, []byte, error) {
	return fixture.CorpusAssets(runner)
}

// Validate admits the complete pinned source without starting any runtime.
func Validate(ctx context.Context, manifest fixtureasset.Manifest, content []byte) error {
	if ctx == nil {
		return fmt.Errorf("fixture corpus: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fixture.ValidateCorpusAsset(manifest, content); err != nil {
		return err
	}
	return ctx.Err()
}
