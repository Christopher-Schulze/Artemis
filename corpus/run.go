package corpus

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/network"
)

// Result retains actual observation and input identity. Passing includes owned
// page/engine/server cleanup and never claims support for another runner.
type Result struct {
	FixtureManifest fixtureasset.Manifest
	StatusCode      int
	URL             string
	Title           string
	Text            string
	HTML            string
	Markdown        string
	EvalResult      string
	EvalError       string
	Links           []string
	Passed          bool
	DurationMs      int64
}

// RunRenderless admits the canonical input before owning a fresh scoped run.
// Native/other runner manifests fail explicitly instead of changing engines.
func RunRenderless(ctx context.Context, manifest fixtureasset.Manifest, content []byte) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture corpus execution: owning context required")
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
	if manifest.RunnerRef != "artemis:fixture:renderless" {
		return result, fmt.Errorf("fixture corpus execution: renderless input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("renderless", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture corpus execution: zero passing coverage: %+v", eligibility))
	}
	return runAdmittedRenderless(ctx, manifest, content, scenario)
}

func runAdmittedRenderless(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario) (result Result, runErr error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	server, err := fixture.NewServerFromCorpus(content)
	if err != nil {
		return result, err
	}
	defer server.Close()
	runner, err := newCorpusEngine(server.BaseURL())
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, runner.Close()) }()
	observed, err := fixture.RunEngineScenario(ctx, runner, scenario, server)
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown,
		EvalResult: observed.EvalResult, EvalError: observed.EvalErr, Links: append([]string{}, observed.Links...)}
	if err != nil {
		return result, err
	}
	return result, fixture.ValidateCrossResult(scenario, observed)
}

func newCorpusEngine(rawURL string) (*engine.Engine, error) {
	policy, err := corpusNetworkPolicy(rawURL)
	if err != nil {
		return nil, err
	}
	return engine.New(engine.Config{PolicyConfig: policy, Timeout: 30 * time.Second, MaxBodyBytes: 8 << 20, MaxDownloadDiskBytes: 1,
		JSContextPoolSize: 1, JSContextPoolWarm: false,
		SessionBudget: engine.SessionBudget{MaxTabs: 1, MaxRequests: 64, MaxConcurrency: 4, MaxResponseBytes: 64 << 20, MaxDiskBytes: 1, Timeout: 30 * time.Second}})
}

func corpusNetworkPolicy(rawURL string) (network.PolicyConfig, error) {
	origin, err := url.Parse(rawURL)
	if err != nil {
		return network.PolicyConfig{}, err
	}
	port, err := strconv.Atoi(origin.Port())
	if err != nil {
		return network.PolicyConfig{}, err
	}
	return network.PolicyConfig{AllowedDomains: []string{origin.Hostname()}, AllowedPorts: []int{port}, AllowedSchemes: []string{"http"},
		AllowedMethods: []string{"GET", "POST"}, AllowPrivateNetworks: true, MaxResponseBodyBytes: 8 << 20}, nil
}
