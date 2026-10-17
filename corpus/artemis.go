package corpus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
)

// RunArtemis executes the exact admitted case through the existing BrowserRuntime
// session/page authority, with an owned disposable metadata/profile directory.
func RunArtemis(ctx context.Context, manifest fixtureasset.Manifest, content []byte, launch process.LaunchConfig) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture BrowserRuntime execution: owning context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	defer func() {
		runErr = errors.Join(runErr, ctx.Err())
		result.Passed, result.DurationMs = runErr == nil, time.Since(started).Milliseconds()
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if manifest.RunnerRef != "artemis:fixture:artemis" {
		return result, fmt.Errorf("fixture BrowserRuntime execution: Artemis input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("artemis", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture BrowserRuntime execution: zero passing coverage: %+v", eligibility))
	}
	if err := validateCorpusLaunch(launch); err != nil {
		return result, err
	}
	return runAdmittedArtemis(ctx, manifest, content, scenario, launch)
}

func runAdmittedArtemis(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario, launch process.LaunchConfig) (result Result, runErr error) {
	server, err := fixture.NewServerFromCorpus(content)
	if err != nil {
		return result, err
	}
	defer server.Close()
	launch.NetworkPolicy, err = corpusLaunchPolicy(server.BaseURL())
	if err != nil {
		return result, err
	}
	launch, err = prepareCorpusLaunch(launch)
	if err != nil {
		return result, err
	}
	root, err := os.MkdirTemp("", "artemis-corpus-runtime-")
	if err != nil {
		return result, err
	}
	identity, err := os.Lstat(root)
	if err != nil {
		return result, fmt.Errorf("fixture BrowserRuntime directory identity: %w", err)
	}
	defer func() {
		current, err := os.Lstat(root)
		if err != nil || !current.IsDir() || !os.SameFile(identity, current) {
			runErr = errors.Join(runErr, err, fmt.Errorf("fixture BrowserRuntime directory identity changed; cleanup denied"))
			return
		}
		runErr = errors.Join(runErr, os.RemoveAll(root))
	}()
	manager, err := profile.NewRuntimeManager(root)
	if err != nil {
		return result, err
	}
	runtime, err := profile.NewBrowserRuntime(manager)
	if err != nil {
		return result, err
	}
	return runArtemisSession(ctx, runtime, manifest, scenario, server, launch)
}

func runArtemisSession(ctx context.Context, runtime *profile.BrowserRuntime, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server, launch process.LaunchConfig) (result Result, runErr error) {
	session, err := runtime.Open(ctx, profile.OpenSessionRequest{
		ProfileID: "fixture-artemis", OwnerUserRef: "fixture-owner", Class: profile.ProfileEphemeral,
		Lifetime: 30 * time.Second, Limits: profile.ResourceLimits{MaxPages: 1, MaxLifetime: 30 * time.Second},
	}, launch)
	if err != nil {
		return result, err
	}
	defer func() {
		// BrowserRuntime synchronously closes the browser before its manager
		// checks this context. Its owned teardown supplies the actual bounds;
		// an earlier short timer cannot cancel it and only rejects metadata
		// cleanup after a successfully completed browser shutdown.
		if err := runtime.Close(context.Background(), session.ID, session.OwnerUserRef); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("fixture BrowserRuntime close session: %w", err))
		}
	}()
	pageID, page, err := runtime.NewPage(ctx, session.ID, session.OwnerUserRef, "about:blank")
	if err != nil {
		return result, err
	}
	defer func() {
		if err := runtime.ClosePage(session.ID, pageID, session.OwnerUserRef); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("fixture BrowserRuntime close page: %w", err))
		}
	}()
	observed, err := fixture.RunNativeScenario(ctx, page, scenario, server.URL(scenario.Path))
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown,
		EvalResult: observed.EvalResult, EvalError: observed.EvalErr, Links: append([]string{}, observed.Links...)}
	if err != nil {
		return result, fmt.Errorf("fixture BrowserRuntime observe page: %w", err)
	}
	return result, fixture.ValidateCrossResult(scenario, observed)
}
