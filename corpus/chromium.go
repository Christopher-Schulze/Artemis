package corpus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/process"
)

// RunChromium uses the existing isolated bridge/process owner and the same
// navigation observer as the native corpus adapters. No renderer fallback.
func RunChromium(ctx context.Context, manifest fixtureasset.Manifest, content []byte, launch process.LaunchConfig) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture Chromium execution: owning context required")
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
	if manifest.RunnerRef != "artemis:fixture:chromium" {
		return result, fmt.Errorf("fixture Chromium execution: Chromium input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("chromium", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture Chromium execution: zero passing coverage: %+v", eligibility))
	}
	return runAdmittedChromium(ctx, manifest, content, scenario, launch)
}

func runAdmittedChromium(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario, launch process.LaunchConfig) (result Result, runErr error) {
	if err := validateCorpusLaunch(launch); err != nil {
		return result, err
	}
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
	return runChromiumPage(ctx, manifest, scenario, server, launch)
}

func prepareCorpusLaunch(launch process.LaunchConfig) (process.LaunchConfig, error) {
	if launch.BinaryPath == "" {
		binary, err := process.DiscoverBinary("")
		if err != nil {
			return launch, err
		}
		launch.BinaryPath = binary.Path
	}
	launch.Headless = true
	if launch.StartupTimeout == 0 {
		launch.StartupTimeout = 15 * time.Second
	}
	if launch.ShutdownTimeout == 0 {
		launch.ShutdownTimeout = 5 * time.Second
	}
	if launch.ResourceBudget.SessionTimeout == 0 {
		launch.ResourceBudget.SessionTimeout = 30 * time.Second
	}
	return launch, nil
}

func runChromiumPage(ctx context.Context, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server, launch process.LaunchConfig) (result Result, runErr error) {
	browser, err := bridge.LaunchChromium(ctx, launch)
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, browser.Close()) }()
	browserContext, err := browser.NewContext(ctx)
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, browserContext.Close()) }()
	page, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, page.Close()) }()
	observed, err := fixture.RunNativeScenario(ctx, page, scenario, server.URL(scenario.Path))
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown,
		EvalResult: observed.EvalResult, EvalError: observed.EvalErr, Links: append([]string{}, observed.Links...)}
	if err != nil {
		return result, err
	}
	return result, fixture.ValidateCrossResult(scenario, observed)
}

func validateCorpusLaunch(launch process.LaunchConfig) error {
	if launch.UserDataDir != "" || len(launch.ExtraArgs) != 0 || launch.PolicyProxyURL != "" || launch.NetworkPolicy != nil || len(launch.AllowedPorts) != 0 || launch.AllowPrivateNetworks || launch.Sandbox == process.SandboxDisabled {
		return fmt.Errorf("fixture Chromium execution requires an owned profile, sandbox and origin policy")
	}
	if launch.StartupTimeout < 0 || launch.StartupTimeout > 15*time.Second || launch.ShutdownTimeout < 0 || launch.ShutdownTimeout > 5*time.Second || launch.ResourceBudget.SessionTimeout < 0 || launch.ResourceBudget.SessionTimeout > 30*time.Second {
		return fmt.Errorf("fixture Chromium execution exceeds bounded startup/shutdown/session time")
	}
	return nil
}

func corpusLaunchPolicy(origin string) (*network.PolicyConfig, error) {
	policy, err := corpusNetworkPolicy(origin)
	if err != nil {
		return nil, err
	}
	return &policy, nil
}
