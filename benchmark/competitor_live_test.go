//go:build artemis_live_competitor

package benchmark

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pinnedLiveConfig(t *testing.T) CompetitorConfig {
	t.Helper()
	path, digest := os.Getenv("ARTEMIS_BENCHMARK_BINARY"), os.Getenv("ARTEMIS_BENCHMARK_SHA256")
	if path == "" || digest == "" {
		t.Fatal("live proof requires explicit binary and pinned digest")
	}
	cfg := CompetitorConfig{BinaryDir: filepath.Dir(path), ExpectedSHA256: digest, Timeout: 5 * time.Second}
	if NewCompetitorRunner(cfg).BinaryPath() != path {
		t.Fatalf("unexpected binary basename: %s", path)
	}
	return cfg
}

func TestPinnedCompetitorLiveCDPSemanticsAndCleanup(t *testing.T) {
	runner := NewCompetitorRunner(pinnedLiveConfig(t))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := runner.EnsureBinary(ctx); err != nil {
		t.Fatal(err)
	}
	version, err := runner.Version(ctx)
	if err != nil || version == "" {
		t.Fatalf("actual version: %q, %v", version, err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture := NewArtemisRunner(DefaultScenarios()[:1], false)
	t.Cleanup(func() {
		if err := fixture.Close(); err != nil {
			t.Error(err)
		}
	})
	scenario := DefaultScenarios()[0]
	result := runner.RunScenario(ctx, scenario, fixture.BaseURL()+"/"+scenario.ID+"?first=1&second=2")
	if !result.OK || !result.Validated || result.Validation.Title != scenario.ExpectTitle {
		t.Fatalf("real CDP outcome: %+v", result)
	}
	scenario.ExpectTitle = "Required title that the real fixture does not contain"
	result = runner.RunScenario(ctx, scenario, fixture.BaseURL()+"/"+scenario.ID)
	if result.OK || result.Validated || result.Error == "" {
		t.Fatalf("HTTP success bypassed semantic assertions: %+v", result)
	}
	process := runner.cmd.Process
	done := runner.waitDone
	if err := runner.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("stop returned before process wait")
	}
	if runner.cmd != nil || runner.transport != nil {
		t.Fatal("owned process or transport retained after stop")
	}
	if err := process.Kill(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("process remained live after stop: %v", err)
	}
}

func TestPinnedCompetitorLiveCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-req.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	runner := NewCompetitorRunner(pinnedLiveConfig(t))
	if err := runner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Stop(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan ScenarioResult, 1)
	go func() { result <- runner.RunScenario(ctx, Scenario{ID: "cancel", ExpectTitle: "unreachable"}, srv.URL) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real competitor did not request fixture")
	}
	cancel()
	select {
	case actual := <-result:
		if actual.OK || actual.Error == "" {
			t.Fatalf("cancelled operation succeeded: %+v", actual)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled CDP operation did not join")
	}
}

func TestPinnedCompetitorLiveOccupiedPortAndUnexpectedExit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	}()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("unexpected listener address")
	}
	cfg := pinnedLiveConfig(t)
	cfg.Port = address.Port
	runner := NewCompetitorRunner(cfg)
	if err := runner.Start(t.Context()); err == nil || runner.cmd != nil {
		t.Fatal("occupied endpoint mistaken for owned readiness")
	}
	cfg.Port = 0
	runner = NewCompetitorRunner(cfg)
	if err := runner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runner.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("unexpected process death not observed")
	}
	if err := runner.Stop(); err == nil || !strings.Contains(err.Error(), "before owned stop") {
		t.Fatalf("unexpected exit hidden: %v", err)
	}
	if runner.cmd != nil || runner.transport != nil {
		t.Fatal("unexpected exit retained ownership")
	}
}

func TestPinnedCompetitorLivePairedSamplesAndFailingBudget(t *testing.T) {
	for _, budgeted := range []bool{false, true} {
		name := "paired"
		if budgeted {
			name = "unsupported-budget"
		}
		t.Run(name, func(t *testing.T) {
			cfg := DefaultHarnessConfig()
			cfg.OutputDir, cfg.Iterations = t.TempDir(), 5
			cfg.Competitor, cfg.RequireHeadToHead = pinnedLiveConfig(t), true
			cfg.Workloads = WorkloadsWithIterations(DefaultScenarios()[:1], 5)
			if budgeted {
				cfg.RegressionBudgets = []RegressionBudget{{MetricKind: MetricCPUMs, MaxDelta: 1}}
			}
			sc, err := NewHarness(cfg).Run(t.Context())
			if budgeted && (err == nil || !strings.Contains(err.Error(), "budget failed")) {
				t.Fatalf("failed regression did not fail command: %v", err)
			}
			if !budgeted && err != nil {
				t.Fatal(err)
			}
			if sc == nil || !sc.Honest || len(sc.Samples) != 20 || len(sc.Priming) != 2 || len(sc.Results) != 4 {
				t.Fatalf("real paired matrix incomplete: %+v", sc)
			}
			if sc.CompetitorSHA256 != cfg.Competitor.ExpectedSHA256 || sc.CompetitorVersion == "" {
				t.Fatal("executed provenance not recorded")
			}
		})
	}
}
