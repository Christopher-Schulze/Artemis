package wpt

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

//go:embed testdata/wpt
var testdata embed.FS

const testHarnessShim = `self = globalThis;
(function() {
  var __origSetTimeout = globalThis.setTimeout;
  globalThis.setTimeout = function(fn, delay) {
    if (delay === 0 || delay === undefined) {
      return __origSetTimeout(fn, 0);
    }
    return 0;
  };
})();
`

// Result is the observed outcome of a single WPT test.
type Result struct {
	Path          string
	Name          string
	Status        string
	Message       string
	HarnessStatus string
}

// Runner serves a pinned WPT subset from the embedded testdata mirror and
// executes it against an Artemis engine.
type Runner struct {
	Engine *engine.Engine
	Server *fixture.Server
}

// NewRunner creates a runner with a dedicated fixture server and engine.
func NewRunner() (*Runner, error) {
	srv := fixture.NewServer()
	eng, err := engine.New(engine.Config{PolicyConfig: srv.PolicyConfig()})
	if err != nil {
		srv.Close()
		return nil, fmt.Errorf("engine: %w", err)
	}
	r := &Runner{Engine: eng, Server: srv}
	if err := r.registerFiles(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the engine and fixture server.
func (r *Runner) Close() error {
	var err error
	if r.Engine != nil {
		err = r.Engine.Close()
	}
	if r.Server != nil {
		r.Server.Close()
	}
	return err
}

// Run executes the subset and returns the result for each test case.
func (r *Runner) Run(ctx context.Context, subset Subset) ([]Result, error) {
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
func (r *Runner) RunCase(ctx context.Context, tc TestCase) (Result, error) {
	pageURL := r.Server.URL(tc.Path)
	page, err := r.Engine.Fetch(ctx, pageURL, engine.FetchOpts{RunScripts: true})
	if err != nil {
		return Result{}, fmt.Errorf("fetch %s: %w", tc.Path, err)
	}
	defer page.Close()

	if _, err := page.Eval(ctx, "window.dispatchEvent(new Event('load'))"); err != nil {
		return Result{}, fmt.Errorf("dispatch load %s: %w", tc.Path, err)
	}

	v, err := page.Eval(ctx, "JSON.stringify(__wptResults)")
	if err != nil {
		return Result{}, fmt.Errorf("read results %s: %w", tc.Path, err)
	}

	raw := v.String()
	if raw == "undefined" || raw == "null" || raw == "" {
		return Result{
			Path:   tc.Path,
			Name:   tc.Name,
			Status: "NO_RESULTS",
		}, nil
	}

	var report struct {
		HarnessStatus  int    `json:"harnessStatus"`
		HarnessMessage string `json:"harnessMessage"`
		Tests          []struct {
			Name    string `json:"name"`
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"tests"`
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return Result{}, fmt.Errorf("parse results %s: %w", tc.Path, err)
	}

	for _, t := range report.Tests {
		if t.Name == tc.Name {
			return Result{
				Path:          tc.Path,
				Name:          tc.Name,
				Status:        testStatus(t.Status),
				Message:       t.Message,
				HarnessStatus: harnessStatus(report.HarnessStatus),
			}, nil
		}
	}

	return Result{
		Path:          tc.Path,
		Name:          tc.Name,
		Status:        "NOT_FOUND",
		HarnessStatus: harnessStatus(report.HarnessStatus),
	}, nil
}

// registerFiles walks the embedded testdata mirror and registers every file
// on the fixture server so the engine can fetch it at the same path the
// upstream WPT test expects.
func (r *Runner) registerFiles() error {
	return fs.WalkDir(testdata, "testdata/wpt", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, "testdata/wpt/")
		content, err := testdata.ReadFile(p)
		if err != nil {
			return err
		}

		if rel == "resources/testharness.js" {
			content = append([]byte(testHarnessShim), content...)
		}

		ct := contentType(rel)
		r.Server.RegisterRaw("/"+rel, ct, 200, content)
		return nil
	})
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
