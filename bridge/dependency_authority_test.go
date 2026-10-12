package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type testDependencyAuthorizer struct{}

func (testDependencyAuthorizer) Authorize(ctx context.Context, artifact, version, path string) error {
	if artifact == "" || version == "" || !filepath.IsAbs(path) {
		return errors.New("invalid dependency authorization request")
	}
	return ctx.Err()
}

func testBridgeInitConfig() BridgeInitConfig {
	return BridgeInitConfig{
		Headless: true, DependencyAuthorizer: testDependencyAuthorizer{}, ArtifactVersion: "test-1",
	}
}

func TestDependencyAuthorizerTypeIsUsable(t *testing.T) {
	if err := (testDependencyAuthorizer{}).Authorize(context.Background(), "chromium", "test-1", filepath.Join("/", "tmp", "chromium")); err != nil {
		t.Fatal(err)
	}
}
