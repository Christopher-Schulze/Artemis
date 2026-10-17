package corpus

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/serve"
)

// RunServe executes through the real authenticated listener and existing
// serialized client. Admission precedes every listener/session/page resource.
func RunServe(ctx context.Context, manifest fixtureasset.Manifest, content []byte) (result Result, runErr error) {
	if ctx == nil {
		return result, fmt.Errorf("fixture Serve execution: owning context required")
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
	if manifest.RunnerRef != "artemis:fixture:serve" {
		return result, fmt.Errorf("fixture Serve execution: Serve input required; no runner fallback")
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return result, err
	}
	eligibility, err := fixture.AssessEligibility("serve", scenario, time.Now())
	if err != nil || eligibility.State != fixture.EligibilityReady {
		return result, errors.Join(err, fmt.Errorf("fixture Serve execution: zero passing coverage: %+v", eligibility))
	}
	return runAdmittedServe(ctx, manifest, content, scenario)
}

func runAdmittedServe(ctx context.Context, manifest fixtureasset.Manifest, content []byte, scenario fixture.Scenario) (result Result, runErr error) {
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
	owner, err := artemis.NewAgent(artemis.AgentConfig{MaxSessions: 1, MaxTabs: 1,
		FetchTimeout: 30 * time.Second, ScriptTimeout: 30 * time.Second, PolicyConfig: policy, PolicyEngine: actions,
		SessionBudget: engine.SessionBudget{MaxTabs: 1, MaxRequests: 64, MaxConcurrency: 4,
			MaxResponseBytes: 64 << 20, MaxDiskBytes: 1, Timeout: 30 * time.Second}})
	if err != nil {
		return result, err
	}
	if err := owner.Start(ctx); err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, owner.Stop()) }()
	return runControlServe(ctx, owner, manifest, scenario, server)
}

func runControlServe(ctx context.Context, owner *artemis.Agent, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server) (result Result, runErr error) {
	runCtx, cancel := context.WithCancel(ctx)
	ready, done := make(chan serve.Readiness, 1), make(chan struct{})
	token := rand.Text()
	controller := serve.New(owner, serve.Opts{AuthToken: token, RateLimit: serve.RateLimit{Burst: 32, ClientBurst: 32}, Ready: func(info serve.Readiness) error {
		if err := info.Validate(); err != nil {
			return err
		}
		ready <- info
		return nil
	}})
	var serveErr error
	go func() {
		serveErr = controller.ListenAndServe(runCtx, "127.0.0.1:0")
		close(done)
	}()
	defer func() {
		cancel()
		closeErr := controller.Close()
		<-done
		if serveErr == context.Canceled {
			serveErr = nil
		}
		runErr = errors.Join(runErr, closeErr, serveErr)
	}()
	select {
	case info := <-ready:
		return runServeConnection(ctx, info, token, manifest, scenario, server)
	case <-done:
		return result, errors.Join(serveErr, fmt.Errorf("fixture Serve owner stopped before readiness"))
	case <-ctx.Done():
		return result, ctx.Err()
	}
}

func runServeConnection(ctx context.Context, info serve.Readiness, token string, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server) (result Result, runErr error) {
	client, err := serve.Dial(ctx, "ws://"+info.Address+"/", token, "")
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, client.Close()) }()
	if client.ServerInstanceID() != info.InstanceID || client.ServerProcessID() != info.ProcessID {
		return result, fmt.Errorf("fixture Serve readiness and authenticated owner differ")
	}
	var session serve.SessionNewResult
	if err := client.Call(ctx, serve.CmdSessionNew, serve.SessionNewParams{}, &session); err != nil {
		return result, err
	}
	if session.SessionID == "" {
		return result, fmt.Errorf("fixture Serve session omitted its actual identity")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, client.Call(cleanup, serve.CmdSessionClose, serve.SessionCloseParams{SessionID: session.SessionID}, &serve.EmptyResult{}))
	}()
	return runServePage(ctx, client, session.SessionID, manifest, scenario, server)
}
