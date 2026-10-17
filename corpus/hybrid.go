package corpus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/router"
)

// RunHybrid admits the exact HybridRouter input. Known restrictions and real
// routing failures remain failures with zero passing coverage.
func RunHybrid(ctx context.Context, manifest fixtureasset.Manifest, content []byte, launch process.LaunchConfig) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture hybrid execution: owning context required")
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
	if manifest.RunnerRef != "artemis:fixture:hybrid" {
		return result, fmt.Errorf("fixture hybrid execution: hybrid input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("hybrid", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture hybrid execution: zero passing coverage: %+v", eligibility))
	}
	if err := validateCorpusLaunch(launch); err != nil {
		return result, err
	}
	return runAdmittedHybrid(ctx, manifest, content, scenario, launch)
}

func runAdmittedHybrid(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario, launch process.LaunchConfig) (result Result, runErr error) {
	server, err := fixture.NewServerFromCorpus(content)
	if err != nil {
		return result, err
	}
	defer server.Close()
	renderless, err := newCorpusEngine(server.BaseURL())
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, renderless.Close()) }()
	launch.NetworkPolicy, err = corpusLaunchPolicy(server.BaseURL())
	if err != nil {
		return result, err
	}
	launch, err = prepareCorpusLaunch(launch)
	if err != nil {
		return result, err
	}
	return runHybridPage(ctx, manifest, scenario, server, renderless, launch)
}

func runHybridPage(ctx context.Context, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server, renderless *engine.Engine, launch process.LaunchConfig) (result Result, runErr error) {
	browser, err := bridge.LaunchChromium(ctx, launch)
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, browser.Close()) }()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, owner.Close()) }()
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, page.Close()) }()
	runner, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{
		router.ModeStaticFetch:  router.RuntimeExecutor{Runtime: renderless, RunScripts: true, AsyncFetch: true},
		router.ModeRenderlessJS: router.RuntimeExecutor{Runtime: renderless, RunScripts: true, AsyncFetch: true},
		router.ModeChromiumCDP:  router.ChromiumExecutor{Page: page},
		router.ModeStealth:      router.ChromiumExecutor{Page: page},
	}})
	if err != nil {
		return result, err
	}
	observed, err := fixture.RunHybridScenario(ctx, runner, page, scenario, server)
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown,
		EvalResult: observed.EvalResult, EvalError: observed.EvalErr, Links: append([]string{}, observed.Links...)}
	if err != nil {
		return result, err
	}
	return result, fixture.ValidateCrossResult(scenario, observed)
}
