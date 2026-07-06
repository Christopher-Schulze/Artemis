package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// RunnerConfig configures the smoke runner.
type RunnerConfig struct {
	ArtemisBinPath string        // path to pre-built artemis binary; if empty, runner builds it
	BuildDir       string        // dir to build cmd/artemis from (the artemis module root)
	OutDir         string        // evidence output directory
	StepTimeout    time.Duration // default per-step timeout
	Host           string        // serve host
	Port           int           // serve port
	Logger         *slog.Logger
}

// StepResult is the outcome of a single step.
type StepResult struct {
	Index    int           `json:"index"`
	Name     string        `json:"name"`
	Cmd      string        `json:"cmd"`
	OK       bool          `json:"ok"`
	Duration time.Duration `json:"duration_ms"`
	Error    string        `json:"error,omitempty"`
	Assert   *bool         `json:"assert_pass,omitempty"`
	Got      any           `json:"got,omitempty"`
}

// ScenarioResult is the outcome of a single scenario.
type ScenarioResult struct {
	ID          string        `json:"id"`
	Site        string        `json:"site"`
	Description string        `json:"description"`
	Pass        bool          `json:"pass"`
	Skipped     bool          `json:"skipped"`
	SkipReason  string        `json:"skip_reason,omitempty"`
	NetworkFail bool          `json:"network_fail"` // true if the only failures were network/site-down
	Steps       []StepResult  `json:"steps"`
	Duration    time.Duration `json:"duration_ms"`
	Artifacts   []string      `json:"artifacts"`
}

// Scorecard is the aggregate result document.
type Scorecard struct {
	Version    string           `json:"version"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Total      int              `json:"total"`
	Passed     int              `json:"passed"`
	Failed     int              `json:"failed"`
	Skipped    int              `json:"skipped"`
	Results    []ScenarioResult `json:"results"`
	BinaryPath string           `json:"binary_path"`
}

// Runner drives the smoke loop.
type Runner struct {
	cfg     RunnerConfig
	logger  *slog.Logger
	binPath string
}

func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if cfg.StepTimeout == 0 {
		cfg.StepTimeout = 30 * time.Second
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 9344
	}
	return &Runner{cfg: cfg, logger: cfg.Logger}
}

// BuildArtemis builds cmd/artemis into cfg.ArtemisBinPath (or a temp path).
func (r *Runner) BuildArtemis(ctx context.Context) error {
	if r.cfg.ArtemisBinPath != "" {
		if _, err := os.Stat(r.cfg.ArtemisBinPath); err == nil {
			r.binPath = r.cfg.ArtemisBinPath
			r.logger.Info("using pre-built artemis", "path", r.binPath)
			return nil
		}
	}
	buildDir := r.cfg.BuildDir
	if buildDir == "" {
		return fmt.Errorf("BuildArtemis: BuildDir required when ArtemisBinPath is missing")
	}
	out := r.cfg.ArtemisBinPath
	if out == "" {
		f, err := os.CreateTemp("", "artemis-smoke-*")
		if err != nil {
			return fmt.Errorf("temp bin: %w", err)
		}
		out = f.Name()
		_ = f.Close()
	}
	r.logger.Info("building artemis", "dir", buildDir, "out", out)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/artemis")
	cmd.Dir = buildDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build cmd/artemis: %w; stderr: %s", err, stderr.String())
	}
	r.binPath = out
	return nil
}

// RunAll runs every scenario and writes the scorecard + evidence to OutDir.
func (r *Runner) RunAll(ctx context.Context, sf *ScenarioFile) (*Scorecard, error) {
	if err := os.MkdirAll(r.cfg.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir outdir: %w", err)
	}
	sc := &Scorecard{
		Version:    sf.Version,
		StartedAt:  time.Now(),
		BinaryPath: r.binPath,
		Total:      len(sf.Scenarios),
	}
	for i := range sf.Scenarios {
		s := &sf.Scenarios[i]
		res := r.runScenario(ctx, s)
		sc.Results = append(sc.Results, res)
		if res.Skipped {
			sc.Skipped++
		} else if res.Pass {
			sc.Passed++
		} else {
			sc.Failed++
		}
	}
	sc.FinishedAt = time.Now()
	if err := r.writeScorecard(sc); err != nil {
		return sc, err
	}
	return sc, nil
}

func (r *Runner) writeScorecard(sc *Scorecard) error {
	path := filepath.Join(r.cfg.OutDir, "scorecard.json")
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	r.logger.Info("scorecard written", "path", path,
		"passed", sc.Passed, "failed", sc.Failed, "skipped", sc.Skipped)
	return nil
}

func (r *Runner) runScenario(ctx context.Context, s *Scenario) ScenarioResult {
	return r.runScenarioWithServeAddr(ctx, s, "")
}

// runScenarioWithServeAddr runs a scenario. If serveAddr is non-empty,
// it connects directly to an already-running serve server at that addr
// (host:port) instead of spawning the artemis binary. This is the
// in-process test path.
func (r *Runner) runScenarioWithServeAddr(ctx context.Context, s *Scenario, serveAddr string) ScenarioResult {
	res := ScenarioResult{
		ID:          s.ID,
		Site:        s.Site,
		Description: s.Description,
		Pass:        true,
	}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	// Check required env vars.
	for _, ev := range s.Requires {
		if os.Getenv(ev) == "" {
			res.Skipped = true
			res.SkipReason = fmt.Sprintf("missing env var %s", ev)
			r.logger.Info("scenario skipped (missing env)", "id", s.ID, "env", ev)
			return res
		}
	}

	scenarioDir := filepath.Join(r.cfg.OutDir, s.ID)
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		res.Pass = false
		res.Steps = append(res.Steps, StepResult{
			Name: "mkdir", Cmd: "mkdir", OK: false, Error: err.Error(),
		})
		return res
	}

	addr := serveAddr
	if addr == "" {
		// Start artemis serve subprocess.
		srvCtx, srvCancel := context.WithCancel(ctx)
		defer srvCancel()
		addr = fmt.Sprintf("%s:%d", r.cfg.Host, r.cfg.Port)
		artemisProc := exec.CommandContext(srvCtx, r.binPath, "serve",
			"--host", r.cfg.Host, "--port", fmt.Sprintf("%d", r.cfg.Port))
		var srvLog bytes.Buffer
		artemisProc.Stdout = &srvLog
		artemisProc.Stderr = &srvLog
		if err := artemisProc.Start(); err != nil {
			res.Pass = false
			res.Steps = append(res.Steps, StepResult{
				Name: "start artemis serve", Cmd: "serve", OK: false, Error: err.Error(),
			})
			return res
		}
		defer func() {
			_ = artemisProc.Process.Kill()
			_ = artemisProc.Wait()
			_ = os.WriteFile(filepath.Join(scenarioDir, "serve.log"), srvLog.Bytes(), 0o644)
		}()
		// Wait for /healthz.
		if err := waitForHealth(ctx, "http://"+addr+"/healthz", 10*time.Second); err != nil {
			res.Pass = false
			res.Steps = append(res.Steps, StepResult{
				Name: "wait for serve health", Cmd: "healthz", OK: false, Error: err.Error(),
			})
			return res
		}
	}

	// Connect WS.
	wsURL := "ws://" + addr + "/"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		res.Pass = false
		res.Steps = append(res.Steps, StepResult{
			Name: "ws dial", Cmd: "dial", OK: false, Error: err.Error(),
		})
		return res
	}
	defer conn.CloseNow()
	// Match the server's 8MB read limit for large page dumps.
	conn.SetReadLimit(8 << 20)

	var mu sync.Mutex
	stepTimeout := s.Timeout
	if stepTimeout == 0 {
		stepTimeout = r.cfg.StepTimeout
	}
	// sessionID and pageID are tracked across steps and auto-injected
	// into step params so scenario YAML doesn't need to thread them.
	var sessionID, pageID string
	for i := range s.Steps {
		st := &s.Steps[i]
		// Inject tracked IDs into params (don't overwrite explicit values).
		injectIDs(st, sessionID, pageID)
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		sr := r.runStep(stepCtx, conn, st, &mu)
		cancel()
		sr.Index = i
		res.Steps = append(res.Steps, sr)
		if !sr.OK {
			res.Pass = false
			// Network-failure tolerant: a fetch_failed on page.open is a
			// site/network issue, not an artemis bug. Mark network_fail
			// but still record the failure honestly.
			if st.Cmd == "page.open" && strings.Contains(sr.Error, "fetch_failed") {
				res.NetworkFail = true
			}
			// Stop the scenario on first failure (no point continuing
			// against a broken page).
			break
		}
		// Track session/page IDs from successful responses.
		if st.Cmd == "session.new" {
			if id, ok := extractString(sr.Got, "sessionId"); ok {
				sessionID = id
			}
		}
		if st.Cmd == "page.open" {
			if id, ok := extractString(sr.Got, "pageId"); ok {
				pageID = id
			}
		}
		if st.Cmd == "page.close" {
			pageID = ""
		}
		if st.Cmd == "session.close" {
			sessionID = ""
			pageID = ""
		}
		if st.Cmd == "page.dump" {
			if data, ok := extractString(sr.Got, "data"); ok {
				art := filepath.Join(scenarioDir, fmt.Sprintf("step-%02d-%s.txt", i, sanitize(st.Name)))
				_ = os.WriteFile(art, []byte(data), 0o644)
				res.Artifacts = append(res.Artifacts, art)
			}
		}
	}
	return res
}

// injectIDs sets sessionId/pageId in step params if the step needs them
// and they aren't already set. This lets scenario YAML omit the
// bookkeeping IDs.
func injectIDs(st *Step, sessionID, pageID string) {
	if st.Params == nil {
		st.Params = map[string]any{}
	}
	needsSession := stepNeedsSession(st.Cmd)
	needsPage := stepNeedsPage(st.Cmd)
	if needsSession && sessionID != "" {
		if _, ok := st.Params["sessionId"]; !ok {
			st.Params["sessionId"] = sessionID
		}
	}
	if needsPage && pageID != "" {
		if _, ok := st.Params["pageId"]; !ok {
			st.Params["pageId"] = pageID
		}
	}
}

func stepNeedsSession(cmd string) bool {
	switch cmd {
	case "session.close", "page.open", "page.close", "page.eval",
		"page.dump", "page.click_by_text", "page.type",
		"page.wait_idle", "page.assert":
		return true
	default:
		return false
	}
}

func stepNeedsPage(cmd string) bool {
	switch cmd {
	case "page.close", "page.eval", "page.dump", "page.click_by_text",
		"page.type", "page.wait_idle", "page.assert":
		return true
	default:
		return false
	}
}

func (r *Runner) runStep(ctx context.Context, conn *websocket.Conn, st *Step, mu *sync.Mutex) StepResult {
	sr := StepResult{
		Name: st.Name,
		Cmd:  st.Cmd,
	}
	start := time.Now()
	defer func() { sr.Duration = time.Since(start) }()

	params, err := json.Marshal(st.Params)
	if err != nil {
		sr.Error = "marshal params: " + err.Error()
		return sr
	}
	req := struct {
		ID     string          `json:"id"`
		Cmd    string          `json:"cmd"`
		Params json.RawMessage `json:"params,omitempty"`
	}{
		ID:     fmt.Sprintf("smoke-%s", st.Name),
		Cmd:    st.Cmd,
		Params: params,
	}
	body, _ := json.Marshal(req)

	mu.Lock()
	writeErr := conn.Write(ctx, websocket.MessageText, body)
	mu.Unlock()
	if writeErr != nil {
		sr.Error = "ws write: " + writeErr.Error()
		return sr
	}
	mu.Lock()
	_, data, readErr := conn.Read(ctx)
	mu.Unlock()
	if readErr != nil {
		sr.Error = "ws read: " + readErr.Error()
		return sr
	}
	var resp struct {
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Value any    `json:"value,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		sr.Error = "unmarshal resp: " + err.Error()
		return sr
	}
	sr.OK = resp.OK
	sr.Got = resp.Value
	if !resp.OK && resp.Error != nil {
		sr.Error = resp.Error.Code + ": " + resp.Error.Message
		return sr
	}
	// For page.assert, check the pass field.
	if st.Cmd == "page.assert" && resp.OK {
		if m, ok := resp.Value.(map[string]any); ok {
			pass, _ := m["pass"].(bool)
			sr.Assert = &pass
			want := true
			if st.AssertPass != nil {
				want = *st.AssertPass
			}
			if pass != want {
				sr.OK = false
				sr.Error = fmt.Sprintf("assertion failed: got pass=%v, want %v (got=%v)", pass, want, m["got"])
			}
		}
	}
	return sr
}

func waitForHealth(ctx context.Context, url string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		req, _ := newGetRequest(ctx, url)
		resp, err := defaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("healthz did not come up within %s", maxWait)
}

func extractString(v any, key string) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m[key].(string)
	return s, ok
}

func sanitize(s string) string {
	out := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
	if out == "" {
		return "step"
	}
	return out
}
