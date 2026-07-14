package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// ArtemisRunner runs the Artemis engine against benchmark scenarios.
// It uses a fixture server to serve fixture HTML and measures the
// full fetch→parse→(optional JS)→extract pipeline.
type ArtemisRunner struct {
	engine *engine.Engine
	server *fixture.Server
}

// NewArtemisRunner creates a runner with a pooled V8 engine and a
// fixture server serving the given scenarios. The caller must call
// Close to release resources.
func NewArtemisRunner(scenarios []Scenario) *ArtemisRunner {
	srv := fixture.NewServer()
	for _, s := range scenarios {
		srv.RegisterHTML("/"+s.ID, s.HTML)
	}
	eng, err := engine.New(engine.Config{
		JSContextPoolSize: 8,
		JSContextPoolWarm: false,
		Timeout:           10 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		PolicyConfig:      srv.PolicyConfig(),
	})
	if err != nil {
		srv.Close()
		return &ArtemisRunner{server: srv}
	}
	return &ArtemisRunner{engine: eng, server: srv}
}

// Close releases the engine and server resources.
func (r *ArtemisRunner) Close() {
	if r.engine != nil {
		r.engine.Close()
	}
	if r.server != nil {
		r.server.Close()
	}
}

// BaseURL returns the fixture server URL.
func (r *ArtemisRunner) BaseURL() string {
	return r.server.BaseURL()
}

// RunScenario runs the Artemis engine against a single scenario and
// returns the measured result. The scenarioID must match a scenario
// served by the fixture server.
func (r *ArtemisRunner) RunScenario(ctx context.Context, s Scenario) ScenarioResult {
	result := ScenarioResult{
		ScenarioID: s.ID,
		Engine:     EngineArtemis,
		Timestamp:  time.Now().UTC(),
	}

	if r.engine == nil {
		result.Error = "artemis engine not initialized"
		return result
	}

	scenarioURL := r.server.URL("/" + s.ID)

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	page, err := r.engine.Fetch(ctx, scenarioURL, engine.FetchOpts{RunScripts: s.ScriptCount > 0})
	if err != nil {
		wallMs := float64(time.Since(start).Microseconds()) / 1000.0
		result.WallMs = wallMs
		result.Error = fmt.Sprintf("fetch: %v", err)
		return result
	}

	// Exercise extraction surfaces to measure the full pipeline
	_ = page.Title()
	_ = page.Markdown()
	_ = page.Links()
	_ = page.Text()

	wallMs := float64(time.Since(start).Microseconds()) / 1000.0

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	result.WallMs = wallMs
	result.AllocBytes = int64(memAfter.TotalAlloc - memBefore.TotalAlloc)
	result.AllocCount = int64(memAfter.Mallocs - memBefore.Mallocs)
	result.OK = true
	return result
}

// RunAll runs all scenarios and returns the results.
func (r *ArtemisRunner) RunAll(ctx context.Context, scenarios []Scenario) []ScenarioResult {
	results := make([]ScenarioResult, 0, len(scenarios))
	for _, s := range scenarios {
		results = append(results, r.RunScenario(ctx, s))
	}
	return results
}

// RunScenarioBench is a testing.B-compatible benchmark function that
// runs a single scenario repeatedly. It is used by the `make bench`
// target to get precise allocation counts.
func (r *ArtemisRunner) RunScenarioBench(b *testing.B, s Scenario) {
	b.Helper()
	scenarioURL := r.server.URL("/" + s.ID)
	runScripts := s.ScriptCount > 0
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := r.engine.Fetch(ctx, scenarioURL, engine.FetchOpts{RunScripts: runScripts})
		if err != nil {
			b.Fatalf("fetch %s: %v", s.ID, err)
		}
		_ = page.Title()
		_ = page.Markdown()
		_ = page.Links()
		_ = page.Text()
	}
}

// ParseOnlyBench measures the parse + extract pipeline without network
// I/O. It parses the scenario HTML directly and runs extraction. This
// isolates the engine cost from network RTT.
func ParseOnlyBench(b *testing.B, s Scenario) {
	b.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(s.HTML), "http://benchmark.test/"+s.ID)
	if err != nil {
		b.Fatalf("parse %s: %v", s.ID, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = doc.Title()
		_ = agent.Markdown(doc)
		_ = agent.Links(doc)
		_ = agent.Text(doc)
	}
}

// DOMQueryBench measures DOM query performance on a scenario's HTML.
func DOMQueryBench(b *testing.B, s Scenario) {
	b.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(s.HTML), "http://benchmark.test/"+s.ID)
	if err != nil {
		b.Fatalf("parse %s: %v", s.ID, err)
	}
	root := doc.Root()
	if root == nil {
		b.Fatalf("no root for %s", s.ID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = webapi.GetElementById(root, "container")
		_ = webapi.GetElementsByTagName(root, "div")
		_ = webapi.GetElementsByClassName(root, "card")
	}
}
