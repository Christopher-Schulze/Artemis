//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type testDependencyAuthorizer struct {
	err error
}

func (a testDependencyAuthorizer) Authorize(ctx context.Context, artifact, version, path string) error {
	if artifact == "" || version == "" || !filepath.IsAbs(path) {
		return errors.New("invalid dependency authorization request")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.err
}

func TestLaunchRequiresDependencyAuthorizationWhenConfigured(t *testing.T) {
	script := writeBrowserScript(t, browserReadyScript)
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: script, RequireDependencyAuthorization: true,
	})
	if err == nil || !IsCode(err, ErrorInvalidConfig) {
		t.Fatalf("missing dependency authority error=%v", err)
	}
}

func TestLaunchUsesDependencyAuthorizationBeforeProcessStart(t *testing.T) {
	script := writeBrowserScript(t, browserReadyScript)
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: script, RequireDependencyAuthorization: true,
		DependencyAuthorizer: testDependencyAuthorizer{err: errors.New("denied")}, ArtifactVersion: "test-1",
	})
	if err == nil || !IsCode(err, ErrorLaunchFailed) {
		t.Fatalf("authorization denial error=%v", err)
	}
}
