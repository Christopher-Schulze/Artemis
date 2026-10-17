package smoke

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/serve"
)

func buildRealSmokeArtemis(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artemis")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", path, "../cmd/artemis")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build actual Artemis: %v %s", err, output)
	}
	return path
}

func TestFourRealSmokeProcessesHaveDistinctBoundOwnersAndJoin(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	t.Setenv("ARTEMIS_DIAGNOSTICS_FILE", "")
	t.Setenv("ARTEMIS_SERVE_TOKEN", "")
	results := make(chan ScenarioResult, 4)
	for index := 0; index < cap(results); index++ {
		runner := NewRunner(RunnerConfig{ArtemisBinPath: binary, OutDir: t.TempDir(), StepTimeout: 5 * time.Second})
		t.Cleanup(func() {
			if err := runner.Close(); err != nil {
				t.Error(err)
			}
		})
		if err := runner.BuildArtemis(t.Context()); err != nil {
			t.Fatal(err)
		}
		scenario := Scenario{ID: "owned-loop", Site: "https://example.test", Steps: []Step{
			{Name: "create", Cmd: "session.new"}, {Name: "close", Cmd: "session.close"},
		}}
		go func() { results <- runner.runScenario(t.Context(), &scenario) }()
	}
	addresses, instances, processes := make(map[string]bool), make(map[string]bool), make(map[int]bool)
	for index := 0; index < cap(results); index++ {
		var result ScenarioResult
		select {
		case result = <-results:
		case <-time.After(30 * time.Second):
			t.Fatal("a real owned smoke process failed to join")
		}
		if !result.Pass || result.HarnessError != "" || result.ServeOwner == nil || result.Duration <= 0 {
			t.Fatalf("real owned lifecycle failed: %+v", result)
		}
		owner := result.ServeOwner
		if addresses[owner.Address] || instances[owner.InstanceID] || processes[owner.ProcessID] {
			t.Fatalf("concurrent runs shared an owner: %+v", owner)
		}
		addresses[owner.Address], instances[owner.InstanceID], processes[owner.ProcessID] = true, true, true
		process, err := os.FindProcess(owner.ProcessID)
		if err != nil {
			t.Fatal(err)
		}
		if err := process.Signal(syscall.Signal(0)); err == nil {
			t.Fatalf("joined run left process %d alive", owner.ProcessID)
		}
	}
}

func TestOccupiedForeignEndpointCannotBecomeSmokeReadiness(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	var hits atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Error(err)
		}
	}))
	defer foreign.Close()
	host, port := splitAddr(strings.TrimPrefix(foreign.URL, "http://"))
	runner := NewRunner(RunnerConfig{ArtemisBinPath: binary, Host: host, Port: port, OutDir: t.TempDir()})
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := runner.BuildArtemis(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := runner.runScenario(t.Context(), &Scenario{ID: "foreign", Site: "https://example.test", Steps: []Step{{Name: "create", Cmd: "session.new"}}})
	if result.Pass || result.Skipped || result.HarnessError == "" || result.ServeOwner != nil || hits.Load() != 0 {
		t.Fatalf("foreign service was probed/classified as owned: %+v hits=%d", result, hits.Load())
	}
}

func TestRealOwnedServeCancellationJoinsWaitAndReadiness(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	runner := NewRunner(RunnerConfig{ArtemisBinPath: binary, OutDir: t.TempDir()})
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := runner.BuildArtemis(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	owner, client, err := runner.startServe(ctx)
	if err != nil {
		if owner != nil {
			t.Error(owner.close())
		}
		cancel()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := owner.close(); err == nil {
		t.Fatal("interrupted owner was reported as an ordinary clean run")
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("process Wait was not joined")
	}
	select {
	case <-owner.readyDone:
	default:
		t.Fatal("readiness reader was not joined")
	}
	if owner.command.ProcessState == nil || !owner.command.ProcessState.Exited() {
		t.Fatal("real child did not exit")
	}
}

func TestFourRealSmokeOwnersRejectForeignTokensAndClientIdentities(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	type liveOwner struct {
		runner *Runner
		owner  *ownedSmokeServe
		client *serve.Client
	}
	owners := make([]liveOwner, 0, 4)
	for range cap(owners) {
		runner := prepareEvidenceRunner(t, binary, t.TempDir())
		ownerContext, cancel := context.WithCancel(context.Background())
		owner, client, err := runner.startServe(ownerContext)
		if err != nil {
			cancel()
			if owner != nil {
				t.Error(owner.close())
			}
			t.Fatal(err)
		}
		owners = append(owners, liveOwner{runner, owner, client})
		t.Cleanup(func() {
			defer cancel()
			if err := client.Close(); err != nil {
				t.Error(err)
			}
			if err := owner.close(); err != nil {
				t.Error(err)
			}
		})
	}
	for index, target := range owners {
		for otherIndex, foreign := range owners {
			if index == otherIndex {
				continue
			}
			if target.runner.cfg.AuthToken == foreign.runner.cfg.AuthToken {
				t.Fatal("independent owners share generated credential")
			}
			client, err := serve.Dial(t.Context(), "ws://"+target.owner.ready.Address+"/", foreign.runner.cfg.AuthToken, "")
			if client != nil {
				t.Error(client.Close())
			}
			if err == nil || !strings.Contains(err.Error(), "401") {
				t.Fatalf("foreign token was not denied by exact owned endpoint: %v", err)
			}
		}
		foreign := owners[(index+1)%len(owners)]
		client, err := serve.Dial(t.Context(), "ws://"+target.owner.ready.Address+"/", target.runner.cfg.AuthToken, foreign.client.ClientID())
		if client != nil {
			t.Error(client.Close())
		}
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("foreign signed client identity was not denied: %v", err)
		}
		var session serve.SessionNewResult
		if err := target.client.Call(t.Context(), serve.CmdSessionNew, nil, &session); err != nil {
			t.Fatal(err)
		}
		if session.SessionID == "" || session.OwnerUserRef == "" {
			t.Fatal("own authenticated session became unavailable")
		}
		if err := target.client.Call(t.Context(), serve.CmdSessionClose, serve.SessionCloseParams{SessionID: session.SessionID}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealSmokeChildDeathCannotBecomeSuccessfulTeardown(t *testing.T) {
	runner := prepareEvidenceRunner(t, buildRealSmokeArtemis(t), t.TempDir())
	owner, client, err := runner.startServe(t.Context())
	if err != nil {
		if owner != nil {
			t.Error(owner.close())
		}
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-owner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("killed actual child was not reaped")
	}
	if err := owner.close(); err == nil {
		t.Fatal("actual early child death became successful cleanup")
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
}
