package wpt

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

//go:embed testdata/wpt
var testdata embed.FS

// Result is the observed outcome of a single WPT test.
type Result struct {
	Path            string
	Name            string
	Status          string
	Message         string
	HarnessStatus   string
	FixtureManifest *fixtureasset.Manifest
}

// Validate returns an error if the result is incomplete or inconsistent.
func (r Result) Validate() error {
	if r.Path == "" {
		return fmt.Errorf("wpt Result: Path is empty")
	}
	if r.Name == "" {
		return fmt.Errorf("wpt Result %q: Name is empty", r.Path)
	}
	if r.Status == "" {
		return fmt.Errorf("wpt Result %q/%q: Status is empty", r.Path, r.Name)
	}
	if r.HarnessStatus == "" {
		return fmt.Errorf("wpt Result %q/%q: HarnessStatus is empty", r.Path, r.Name)
	}
	switch r.Status {
	case "PASS", "FAIL", "TIMEOUT", "NOTRUN", "PRECONDITION_FAILED":
	default:
		return fmt.Errorf("wpt Result %q/%q: unsupported test status %q", r.Path, r.Name, r.Status)
	}
	switch r.HarnessStatus {
	case "OK", "ERROR", "TIMEOUT", "PRECONDITION_FAILED":
	default:
		return fmt.Errorf("wpt Result %q/%q: unsupported harness status %q", r.Path, r.Name, r.HarnessStatus)
	}
	if r.Status == "PASS" && r.HarnessStatus != "OK" {
		return fmt.Errorf("wpt Result %q/%q: failed harness cannot substantiate PASS", r.Path, r.Name)
	}
	return nil
}

// Runner serves a pinned WPT subset from the embedded testdata mirror and
// executes it against an Artemis engine.
type Runner struct {
	Engine   *engine.Engine
	Server   *fixture.Server
	input    fixtureInput
	manifest fixtureasset.Manifest
}

// NewRunner creates a runner with a dedicated fixture server and engine.
func NewRunner() (*Runner, error) {
	manifest, content, err := DefaultFixture()
	if err != nil {
		return nil, err
	}
	return NewRunnerFromFixture(context.Background(), manifest, content)
}

// NewRunnerFromFixture admits exact governed bytes before creating resources.
func NewRunnerFromFixture(ctx context.Context, manifest fixtureasset.Manifest, content []byte) (*Runner, error) {
	if ctx == nil {
		return nil, fmt.Errorf("wpt: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, err := decodeFixture(manifest, content)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	srv := fixture.NewServer()
	eng, err := engine.New(engine.Config{PolicyConfig: srv.PolicyConfig()})
	if err != nil {
		srv.Close()
		return nil, fmt.Errorf("engine: %w", err)
	}
	r := &Runner{Engine: eng, Server: srv, input: input, manifest: manifest.Detached()}
	if err := r.registerFiles(); err != nil {
		if closeErr := r.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close WPT runner: %w", closeErr))
		}
		return nil, err
	}
	return r, nil
}

// Close releases the engine and fixture server.
func (r *Runner) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.Engine != nil {
		err = r.Engine.Close()
		r.Engine = nil
	}
	if r.Server != nil {
		r.Server.Close()
		r.Server = nil
	}
	r.input = fixtureInput{}
	r.manifest = fixtureasset.Manifest{}
	return err
}

// Run executes the subset and returns the result for each test case.
func (r *Runner) Run(ctx context.Context, subset Subset) ([]Result, error) {
	if r == nil || r.Engine == nil || r.Server == nil || ctx == nil {
		return nil, fmt.Errorf("wpt: current runner and owning context required")
	}
	if err := subset.Validate(); err != nil {
		return nil, err
	}
	pinned := DefaultSubset()
	if subset.Revision != pinned.Revision || subset.OriginURL != pinned.OriginURL {
		return nil, fmt.Errorf("wpt: subset provenance differs from embedded mirror")
	}
	for _, tc := range subset.Tests {
		if _, err := fs.Stat(testdata, "testdata/wpt/"+tc.Path); err != nil {
			return nil, fmt.Errorf("wpt: test asset %s is absent from embedded mirror: %w", tc.Path, err)
		}
		admitted := false
		for _, pinnedCase := range r.input.Subset.Tests {
			if pinnedCase == tc {
				admitted = true
				break
			}
		}
		if !admitted {
			return nil, fmt.Errorf("wpt: test assertion or capability differs from admitted fixture")
		}
	}
	var out []Result
	for _, tc := range subset.Tests {
		res, err := r.RunCase(ctx, tc)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// RunCase runs a single WPT test case and returns its observed result.
func (r *Runner) RunCase(ctx context.Context, tc TestCase) (result Result, resultErr error) {
	if r == nil || r.Engine == nil || r.Server == nil {
		return Result{}, fmt.Errorf("wpt: initialized runner is required")
	}
	if err := tc.Validate(); err != nil {
		return Result{}, err
	}
	if ctx == nil {
		return Result{}, fmt.Errorf("wpt: execution context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if r.Server.ScenarioByPath("/"+tc.Path) == nil {
		return Result{}, fmt.Errorf("wpt: test asset %s is not registered", tc.Path)
	}
	pageURL := r.Server.URL(tc.Path)
	page, err := r.Engine.Fetch(ctx, pageURL, engine.FetchOpts{RunScripts: true})
	if err != nil {
		return Result{}, fmt.Errorf("fetch %s: %w", tc.Path, err)
	}
	defer func() {
		if closeErr := page.Close(); closeErr != nil {
			result = Result{}
			resultErr = errors.Join(resultErr, fmt.Errorf("close WPT page: %w", closeErr))
		}
	}()

	result, err = awaitReport(ctx, page, tc)
	if err != nil {
		return Result{}, err
	}
	for _, admitted := range r.input.Subset.Tests {
		if admitted == tc {
			manifest := r.manifest.Detached()
			result.FixtureManifest = &manifest
			break
		}
	}
	return result, nil
}

// registerFiles serves the exact admitted bundle bytes at upstream paths.
func (r *Runner) registerFiles() error {
	if r == nil || r.Server == nil || len(r.input.Files) == 0 {
		return fmt.Errorf("wpt: admitted fixture input and server required")
	}
	for _, file := range r.input.Files {
		if err := r.Server.RegisterRaw("/"+file.Path, contentType(file.Path), 200, file.Content); err != nil {
			return err
		}
	}
	return nil
}

func contentType(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(rel, ".html"), strings.HasSuffix(rel, ".htm"):
		return "text/html; charset=utf-8"
	default:
		return "text/plain"
	}
}

func testStatus(s int) string {
	switch s {
	case 0:
		return "PASS"
	case 1:
		return "FAIL"
	case 2:
		return "TIMEOUT"
	case 3:
		return "NOTRUN"
	case 4:
		return "PRECONDITION_FAILED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

func harnessStatus(s int) string {
	switch s {
	case 0:
		return "OK"
	case 1:
		return "ERROR"
	case 2:
		return "TIMEOUT"
	case 3:
		return "PRECONDITION_FAILED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}
