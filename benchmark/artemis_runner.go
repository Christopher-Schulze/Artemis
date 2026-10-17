package benchmark

import (
	"context"
	"fmt"
	"math"
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
	engine            *engine.Engine
	server            *fixture.Server
	initializationErr error
	scenarios         map[string]Scenario
}

const contextPoolSize = 8

// NewArtemisRunner creates a runner with a pooled V8 engine and a
// fixture server serving the given scenarios. The caller must call
// Close to release resources. warmPool pre-builds JS contexts so warm
// runs avoid the context creation cost.
func NewArtemisRunner(scenarios []Scenario, warmPool bool) *ArtemisRunner {
	if len(scenarios) == 0 || len(scenarios) > 1024 {
		return &ArtemisRunner{initializationErr: fmt.Errorf("register benchmark fixture: nonempty bounded scenario batch required")}
	}
	srv := fixture.NewServer()
	registered := make(map[string]Scenario, len(scenarios))
	for _, s := range scenarios {
		if err := srv.RegisterHTML("/"+s.ID, s.HTML); err != nil {
			srv.Close()
			return &ArtemisRunner{server: srv, initializationErr: fmt.Errorf("register benchmark fixture: %w", err)}
		}
		registered[s.ID] = s
	}
	eng, err := engine.New(engine.Config{
		JSContextPoolSize: contextPoolSize,
		JSContextPoolWarm: warmPool,
		Timeout:           10 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		PolicyConfig:      srv.PolicyConfig(),
	})
	if err != nil {
		srv.Close()
		return &ArtemisRunner{server: srv, initializationErr: fmt.Errorf("initialize benchmark engine: %w", err)}
	}
	return &ArtemisRunner{engine: eng, server: srv, scenarios: registered}
}

// Close releases the engine and server resources.
func (r *ArtemisRunner) Close() error {
	var closeErr error
	if r.engine != nil {
		closeErr = r.engine.Close()
		r.engine = nil
	}
	if r.server != nil {
		r.server.Close()
		r.server = nil
	}
	r.scenarios = nil
	return closeErr
}

// Reset releases and recreates the engine so the next run is cold.
// warmPool pre-builds JS contexts for warm runs.
func (r *ArtemisRunner) Reset(warmPool bool) error {
	if r.initializationErr != nil {
		return r.initializationErr
	}
	if r.server == nil {
		return fmt.Errorf("benchmark fixture owner is closed")
	}
	if r.engine != nil {
		if closeErr := r.engine.Close(); closeErr != nil {
			r.engine = nil
			return fmt.Errorf("close engine before reset: %w", closeErr)
		}
		r.engine = nil
	}
	eng, err := engine.New(engine.Config{
		JSContextPoolSize: contextPoolSize,
		JSContextPoolWarm: warmPool,
		Timeout:           10 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		PolicyConfig:      r.server.PolicyConfig(),
	})
	if err != nil {
		return err
	}
	r.engine = eng
	return nil
}

// BaseURL returns the fixture server URL.
func (r *ArtemisRunner) BaseURL() string {
	if r.server == nil {
		return ""
	}
	return r.server.BaseURL()
}

// RunScenario runs the Artemis engine against a single scenario and
// returns the measured result. The scenarioID must match a scenario
// served by the fixture server.
func (r *ArtemisRunner) RunScenario(ctx context.Context, s Scenario) ScenarioResult {
	result := ScenarioResult{
		ScenarioID: s.ID,
		Engine:     EngineArtemis,
		EngineMode: string(s.EngineMode),
		Timestamp:  time.Now().UTC(),
	}

	if r.engine == nil {
		result.Error = "artemis engine not initialized"
		if r.initializationErr != nil {
			result.Error = r.initializationErr.Error()
		}
		return result
	}
	if registered, ok := r.scenarios[s.ID]; !ok || registered != s {
		result.Error = "benchmark scenario differs from registered fixture input"
		return result
	}

	scenarioURL := r.server.URL("/" + s.ID)

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	var page *engine.Page
	var evidence ValidationEvidence
	ms, err := measureFunc(func() error {
		var fetchErr error
		page, fetchErr = r.engine.Fetch(ctx, scenarioURL, engine.FetchOpts{RunScripts: s.ScriptCount > 0})
		if fetchErr != nil {
			return fetchErr
		}
		// Measure the full page lifecycle, including extraction and cleanup.
		evidence = validateDocument(s, page.Document())
		return page.Close()
	})
	result.Validation = evidence
	if err != nil {
		result.WallMs = ms.WallMs
		result.CPUMs = ms.CPUMs
		result.RSSBytes = ms.RSSBytes
		result.Error = fmt.Sprintf("page pipeline: %v", err)
		if page != nil {
			if closeErr := page.Close(); closeErr != nil {
				result.Error = fmt.Sprintf("%s; page close: %v", result.Error, closeErr)
			}
		}
		return result
	}

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
	allocCount := memAfter.Mallocs - memBefore.Mallocs
	if memAfter.TotalAlloc < memBefore.TotalAlloc || memAfter.Mallocs < memBefore.Mallocs ||
		allocBytes > uint64(math.MaxInt64) || allocCount > uint64(math.MaxInt64) {
		result.Error = "memory allocation counters exceeded signed metric range"
		if closeErr := page.Close(); closeErr != nil {
			result.Error = fmt.Sprintf("%s; page close: %v", result.Error, closeErr)
		}
		return result
	}

	result.WallMs = ms.WallMs
	result.CPUMs = ms.CPUMs
	result.RSSBytes = ms.RSSBytes
	result.AllocBytes = int64(allocBytes)
	result.AllocCount = int64(allocCount)
	result.Throughput = ms.Throughput
	result.OK = evidence.Passed
	result.Validated = evidence.Passed
	if !result.OK {
		result.Error = fmt.Sprintf("scenario assertions failed: title=%q links=%d paragraphs=%d", evidence.Title, evidence.Links, evidence.Paragraphs)
	}
	return result
}

func validateScenario(s Scenario, title string, links []agent.Link, text string) bool {
	if title != s.ExpectTitle {
		return false
	}
	if s.ExpectLinks > 0 && len(links) < s.ExpectLinks {
		return false
	}
	if s.ExpectParagraphs > 0 {
		paragraphs := 0
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				paragraphs++
			}
		}
		if paragraphs < s.ExpectParagraphs {
			return false
		}
	}
	return true
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
	if registered, ok := r.scenarios[s.ID]; !ok || registered != s {
		b.Fatal("benchmark scenario differs from registered fixture input")
	}
	scenarioURL := r.server.URL("/" + s.ID)
	runScripts := s.ScriptCount > 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		page, err := r.engine.Fetch(ctx, scenarioURL, engine.FetchOpts{RunScripts: runScripts})
		cancel()
		if err != nil {
			b.Fatalf("fetch %s: %v", s.ID, err)
		}
		_ = page.Title()
		_ = page.Markdown()
		_ = page.Links()
		_ = page.Text()
		if closeErr := page.Close(); closeErr != nil {
			b.Fatalf("close page %s: %v", s.ID, closeErr)
		}
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
