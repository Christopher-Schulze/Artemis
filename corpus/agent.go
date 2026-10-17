package corpus

import (
	"context"
	"errors"
	"fmt"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

// RunAgent executes the exact retained fixture through the existing public
// Agent/Session/ExecuteTask owner. It never substitutes another runner.
func RunAgent(ctx context.Context, manifest fixtureasset.Manifest, content []byte) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture Agent execution: owning context required")
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
	if manifest.RunnerRef != "artemis:fixture:agent" {
		return result, fmt.Errorf("fixture Agent execution: Agent input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("agent", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture Agent execution: zero passing coverage: %+v", eligibility))
	}
	return runAdmittedAgent(ctx, manifest, content, scenario)
}

func runAdmittedAgent(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario) (result Result, runErr error) {
	server, err := fixture.NewServerFromCorpus(content)
	if err != nil {
		return result, err
	}
	defer server.Close()
	policy, err := corpusNetworkPolicy(server.BaseURL())
	if err != nil {
		return result, err
	}
	actions, err := corpusActionPolicy(server.BaseURL())
	if err != nil {
		return result, err
	}
	defer actions.Stop()
	owner, err := artemis.NewAgent(artemis.AgentConfig{
		MaxSessions: 1, MaxTabs: 1, FetchTimeout: 30 * time.Second, ScriptTimeout: 30 * time.Second,
		PolicyConfig: policy, PolicyEngine: actions, SessionBudget: engine.SessionBudget{MaxTabs: 1, MaxRequests: 64,
			MaxConcurrency: 4, MaxResponseBytes: 64 << 20, MaxDiskBytes: 1, Timeout: 30 * time.Second},
	})
	if err != nil {
		return result, err
	}
	if err := owner.Start(ctx); err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, owner.Stop()) }()
	return runAgentTask(ctx, owner, manifest, scenario, server)
}

func runAgentTask(ctx context.Context, owner *artemis.Agent, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server) (result Result, runErr error) {
	session, err := owner.CreateSession("fixture-runner")
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, session.Close()) }()
	task := owner.ExecuteTask(ctx, artemis.Task{ID: scenario.ID, SessionID: session.SessionID(), Timeout: 30 * time.Second,
		Action: artemis.FetchAction{URL: server.URL(scenario.Path), RunScripts: scenario.RunScripts, AsyncFetch: scenario.AsyncFetch, EvaluateExpression: scenario.Expect.Eval}})
	if !task.Success {
		return result, &artemis.TaskError{Code: task.ErrorCode, Op: "fixture execute", Cause: errors.New(task.Error)}
	}
	if task.Data == nil {
		return result, fmt.Errorf("fixture Agent execution: successful task omitted actual page data")
	}
	page := task.Data
	var evaluation string
	if scenario.Expect.Eval != "" {
		if page.Evaluation == nil {
			return result, fmt.Errorf("fixture Agent execution: requested evaluation was omitted")
		}
		evaluation = *page.Evaluation
	}
	links := make([]string, 0, len(page.Links))
	for _, link := range page.Links {
		links = append(links, link.Href)
	}
	observed := fixture.CrossResult{URL: page.URL, StatusCode: page.StatusCode, Title: page.Title,
		Text: page.Text, HTML: page.HTML, Markdown: page.Markdown, Links: links, EvalResult: evaluation}
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown, Links: links, EvalResult: evaluation}
	return result, fixture.ValidateCrossResult(scenario, observed)
}
