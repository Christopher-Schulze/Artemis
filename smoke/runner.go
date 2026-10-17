package smoke

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/serve"
)

// RunnerConfig configures the smoke runner.
type RunnerConfig struct {
	ArtemisBinPath string        // path to pre-built artemis binary; if empty, runner builds it
	BuildDir       string        // dir to build cmd/artemis from (the artemis module root)
	OutDir         string        // evidence output directory
	StepTimeout    time.Duration // default per-step timeout
	Host           string        // serve host
	Port           int           // serve port
	AuthToken      string        // bearer token for the serve control plane
	Logger         *slog.Logger
}

// StepResult is the outcome of a single step.
type StepResult struct {
	Index         int               `json:"index"`
	Name          string            `json:"name"`
	Cmd           string            `json:"cmd"`
	OK            bool              `json:"ok"`
	Duration      int64             `json:"duration_ms"`
	Error         string            `json:"error,omitempty"`
	ErrorCode     string            `json:"error_code,omitempty"`
	FailureKind   serve.FailureKind `json:"failure_kind,omitempty"`
	Outcome       Outcome           `json:"outcome"`
	Assert        *bool             `json:"assert_pass,omitempty"`
	Got           any               `json:"got,omitempty"`
	ValueArtifact *EvidenceArtifact `json:"value_artifact,omitempty"`
}

// ScenarioResult is the outcome of a single scenario.
type ScenarioResult struct {
	ID           string             `json:"id"`
	Site         string             `json:"site"`
	Description  string             `json:"description"`
	Pass         bool               `json:"pass"`
	Skipped      bool               `json:"skipped"`
	SkipReason   string             `json:"skip_reason,omitempty"`
	NetworkFail  bool               `json:"network_fail"` // true if the only failures were network/site-down
	Steps        []StepResult       `json:"steps"`
	Duration     int64              `json:"duration_ms"`
	HarnessError string             `json:"harness_error,omitempty"`
	ServeOwner   *serve.Readiness   `json:"serve_owner,omitempty"`
	Artifacts    []EvidenceArtifact `json:"artifacts"`
	Outcome      Outcome            `json:"outcome"`
}

// Scorecard is the aggregate result document.
type Scorecard struct {
	Version       string           `json:"version"`
	StartedAt     time.Time        `json:"started_at"`
	FinishedAt    time.Time        `json:"finished_at"`
	Total         int              `json:"total"`
	Passed        int              `json:"passed"`
	Failed        int              `json:"failed"`
	Skipped       int              `json:"skipped"`
	Results       []ScenarioResult `json:"results"`
	BinaryPath    string           `json:"binary_path"`
	SchemaVersion int              `json:"schema_version"`
	RunID         string           `json:"run_id"`
	OutputDir     string           `json:"output_dir"`
	Binary        BinaryEvidence   `json:"binary"`
	Inputs        InputEvidence    `json:"inputs"`
	ProductFailed int              `json:"product_failed"`
	NetworkFailed int              `json:"network_failed"`
	HarnessFailed int              `json:"harness_failed"`
}

// Runner drives the smoke loop.
type Runner struct {
	cfg          RunnerConfig
	logger       *slog.Logger
	binPath      string
	binaryDir    string
	binaryInfo   os.FileInfo
	binary       BinaryEvidence
	children     []*ownedSmokeServe
	stageDir     string
	stageInfo    os.FileInfo
	publishedDir string
	reservedInfo os.FileInfo
	outputBytes  int64
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
	if host, err := netip.ParseAddr(cfg.Host); err == nil {
		cfg.Host = host.Unmap().String()
	}
	if cfg.AuthToken == "" {
		cfg.AuthToken = rand.Text()
	}
	return &Runner{cfg: cfg, logger: cfg.Logger}
}

func newSmokeCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable)
	command.Args = append(command.Args, args...)
	browserprocess.ConfigureProcessGroup(command)
	command.Cancel = func() error { return browserprocess.SignalProcessGroup(command.Process, syscall.SIGKILL) }
	command.WaitDelay = smokeShutdownTimeout
	return command
}

// BuildArtemis prepares one owned binary snapshot without altering caller input.
func (r *Runner) BuildArtemis(ctx context.Context) error {
	return r.prepareBinary(ctx)
}

// RunAll runs every scenario and writes the scorecard + evidence to OutDir.
func (r *Runner) RunAll(ctx context.Context, sf *ScenarioFile) (sc *Scorecard, runErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("run context required")
	}
	if err := sf.Validate(); err != nil {
		return nil, err
	}
	manifest, err := sf.fixtureAsset()
	if err != nil {
		return nil, err
	}
	if err := r.verifyBinary(); err != nil {
		return nil, err
	}
	matrix, err := sf.matrixDigest()
	if err != nil {
		return nil, err
	}
	if sf.sourceSHA256 == "" || matrix != sf.normalizedSHA256 {
		return nil, fmt.Errorf("load the actual scenario file; mutated or unbound matrices cannot become evidence")
	}
	configuration, err := ConfigurationDigest(r.cfg.Host, r.cfg.Port, r.cfg.StepTimeout)
	if err != nil {
		return nil, err
	}
	if err := r.beginEvidence(); err != nil {
		return nil, err
	}
	defer func() { runErr = errors.Join(runErr, r.cleanupEvidence()) }()
	sc = &Scorecard{
		Version:       sf.Version,
		StartedAt:     time.Now(),
		BinaryPath:    r.cfg.ArtemisBinPath,
		Total:         len(sf.Scenarios),
		SchemaVersion: 2, RunID: filepath.Base(r.publishedDir), OutputDir: filepath.Join(r.publishedDir, "evidence"),
		Binary: r.binary, Inputs: InputEvidence{FixtureManifest: manifest, ScenarioSHA256: sf.sourceSHA256, MatrixSHA256: matrix, ConfigSHA256: configuration},
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
		if err := ctx.Err(); err != nil {
			sc.HarnessFailed++
			return sc, err
		}
		switch res.Outcome {
		case OutcomeProductFailure:
			sc.ProductFailed++
		case OutcomeNetworkFailure:
			sc.NetworkFailed++
		case OutcomeHarnessFailure, OutcomeInterrupted:
			sc.HarnessFailed++
		}
	}
	if err := r.verifyBinary(); err != nil {
		return sc, err
	}
	if err := r.Close(); err != nil {
		return sc, err
	}
	sc.FinishedAt = time.Now()
	if err := r.writeScorecard(sc); err != nil {
		return sc, err
	}
	return sc, nil
}

func (r *Runner) runScenario(ctx context.Context, s *Scenario) ScenarioResult {
	return r.runScenarioWithServeAddr(ctx, s, "")
}

// runScenarioWithServeAddr runs a scenario. If serveAddr is non-empty,
// it connects directly to an already-running serve server at that addr
// (host:port) instead of spawning the artemis binary. This is the
// in-process test path.
func (r *Runner) runScenarioWithServeAddr(ctx context.Context, s *Scenario, serveAddr string) (res ScenarioResult) {
	res = ScenarioResult{
		ID:          s.ID,
		Site:        s.Site,
		Description: s.Description,
		Pass:        true,
	}
	start := time.Now()
	defer func() { res.finalize(time.Since(start), ctx.Err()) }()

	// Check required env vars.
	for _, ev := range s.Requires {
		if os.Getenv(ev) == "" {
			res.Skipped = true
			res.SkipReason = fmt.Sprintf("missing env var %s", ev)
			r.logger.Info("scenario skipped (missing env)", "id", s.ID, "env", ev)
			return res
		}
	}

	scenarioDir := filepath.Join(r.outputRoot(), s.ID)
	if err := os.Mkdir(scenarioDir, 0o700); err != nil {
		res.failHarness("create scenario directory", err)
		return res
	}
	defer func() {
		if err := syncSmokeDirectory(scenarioDir); err != nil {
			res.failHarness("scenario evidence sync", err)
		}
	}()

	var client *serve.Client
	var err error
	if serveAddr == "" {
		owner, connected, startErr := r.startServe(ctx)
		if owner != nil {
			defer func() {
				closeErr := owner.close()
				logs, logErr := owner.logs.snapshot()
				artifact, writeErr := r.persistArtifact(filepath.Join(s.ID, "serve.log"), logs)
				if writeErr == nil {
					res.Artifacts = append(res.Artifacts, artifact)
				}
				if err := errors.Join(closeErr, logErr, writeErr); err != nil {
					res.failHarness("serve cleanup", err)
				}
			}()
			if startErr == nil {
				res.ServeOwner = &owner.ready
			}
		}
		client, err = connected, startErr
	} else {
		client, err = serve.Dial(ctx, "ws://"+serveAddr+"/", r.cfg.AuthToken, "")
	}
	if err != nil {
		res.failHarness("owned serve connection", err)
		return res
	}
	defer func() {
		if err := client.Close(); err != nil {
			res.failHarness("connection close", err)
		}
	}()
	stepTimeout := s.Timeout
	if stepTimeout == 0 {
		stepTimeout = r.cfg.StepTimeout
	}
	// sessionID, pageID, and ownerUserRef are tracked across steps and auto-injected
	// into step params so scenario YAML doesn't need to thread them.
	var sessionID, pageID, ownerUserRef string
	defer func() {
		if sessionID == "" {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), smokeShutdownTimeout)
		defer cancel()
		if err := client.Call(closeCtx, serve.CmdSessionClose, serve.SessionCloseParams{SessionID: sessionID}, nil); err != nil {
			res.failHarness("session close", err)
		}
	}()
	for i := range s.Steps {
		st, cloneErr := cloneSmokeStep(s.Steps[i])
		if cloneErr != nil {
			res.failHarness("scenario parameters", cloneErr)
			break
		}
		// Inject tracked IDs into params (don't overwrite explicit values).
		injectIDs(st, sessionID, pageID, ownerUserRef)
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		sr := r.runStep(stepCtx, client, st)
		cancel()
		sr.Index = i
		// Capture ownership before bounded evidence can omit a large response.
		if sr.OK {
			switch st.Cmd {
			case "session.new":
				sessionID, _ = extractString(sr.Got, "sessionId")
				ownerUserRef, _ = extractString(sr.Got, "ownerUserRef")
			case "page.open":
				pageID, _ = extractString(sr.Got, "pageId")
			case "page.close":
				pageID = ""
			case "session.close":
				sessionID, pageID, ownerUserRef = "", "", ""
			}
		}
		if sr.Got != nil {
			encoded, err := json.Marshal(sr.Got)
			if err != nil {
				res.failHarness("encode actual result", err)
				break
			}
			if len(encoded) > smokeResultPreviewBytesLimit || st.Cmd == "page.dump" {
				artifact, err := r.persistArtifact(filepath.Join(s.ID, fmt.Sprintf("step-%02d-%s.json", i, sanitize(st.Name))), encoded)
				if err != nil {
					res.failHarness("persist actual result", err)
					break
				}
				res.Artifacts = append(res.Artifacts, artifact)
				sr.ValueArtifact = &artifact
				if len(encoded) > smokeResultPreviewBytesLimit {
					sr.Got = nil
				}
			}
		}
		res.Steps = append(res.Steps, sr)
		if !sr.OK {
			res.Pass = false
			if sr.Outcome == OutcomeHarnessFailure {
				res.failHarness("command protocol/transport", errors.New(sr.Error))
			}
			// Tolerance requires the canonical wire's observed network cause.
			if st.Cmd == "page.open" && sr.ErrorCode == string(serve.ErrFetchFailed) && sr.FailureKind == serve.FailureNetwork {
				res.NetworkFail = true
				if s.NetworkFail != "strict" {
					res.Skipped = true
					res.SkipReason = "site/network unavailable: " + sr.Error
				}
			}
			// Stop the scenario on first failure (no point continuing
			// against a broken page).
			break
		}
	}
	return res
}

func (r *ScenarioResult) failHarness(operation string, err error) {
	r.Pass, r.Skipped = false, false
	r.SkipReason = ""
	message := operation + ": " + err.Error()
	if r.HarnessError != "" {
		message = r.HarnessError + "; " + message
	}
	r.HarnessError = message
	r.Steps = append(r.Steps, StepResult{Name: operation, Cmd: "harness", Error: err.Error(), Outcome: OutcomeHarnessFailure})
}

func cloneSmokeStep(value Step) (*Step, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var cloned Step
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

// injectIDs sets sessionId/pageId/ownerUserRef in step params if the step needs them
// and they aren't already set. This lets scenario YAML omit the
// bookkeeping IDs.
func injectIDs(st *Step, sessionID, pageID, ownerUserRef string) {
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
	if st.Cmd == "session.close" && ownerUserRef != "" {
		if _, ok := st.Params["ownerUserRef"]; !ok {
			st.Params["ownerUserRef"] = ownerUserRef
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

func (r *Runner) runStep(ctx context.Context, client *serve.Client, st *Step) (sr StepResult) {
	sr = StepResult{
		Name: st.Name,
		Cmd:  st.Cmd,
	}
	start := time.Now()
	defer func() {
		sr.Duration = time.Since(start).Milliseconds()
		if sr.Outcome == "" {
			if sr.OK {
				sr.Outcome = OutcomePass
			} else {
				sr.Outcome = OutcomeProductFailure
			}
		}
	}()
	if err := client.Call(ctx, serve.Command(st.Cmd), st.Params, &sr.Got); err != nil {
		sr.Error = err.Error()
		var remote *serve.Err
		if errors.As(err, &remote) {
			sr.ErrorCode, sr.FailureKind = remote.Code, remote.FailureKind
			if remote.Code == string(serve.ErrFetchFailed) && remote.FailureKind == serve.FailureNetwork {
				sr.Outcome = OutcomeNetworkFailure
			}
		} else {
			sr.Outcome = OutcomeHarnessFailure
		}
		return sr
	}
	sr.OK = true
	if err := validateSmokeIdentity(st.Cmd, sr.Got); err != nil {
		sr.OK, sr.Error, sr.Outcome = false, err.Error(), OutcomeHarnessFailure
		return sr
	}
	// For page.assert, check the pass field.
	if st.Cmd == "page.assert" {
		if m, ok := sr.Got.(map[string]any); ok {
			pass, valid := m["pass"].(bool)
			if !valid {
				sr.OK = false
				sr.Error = "assert response missing boolean pass"
				sr.Outcome = OutcomeHarnessFailure
				return sr
			}
			sr.Assert = &pass
			want := true
			if st.AssertPass != nil {
				want = *st.AssertPass
			}
			if pass != want {
				sr.OK = false
				sr.Error = fmt.Sprintf("assertion failed: got pass=%v, want %v (got=%v)", pass, want, m["got"])
			}
		} else {
			sr.OK = false
			sr.Error = "assert response is not an object"
			sr.Outcome = OutcomeHarnessFailure
		}
	}
	return sr
}

func validateSmokeIdentity(command string, value any) error {
	var fields []string
	switch command {
	case "session.new":
		fields = []string{"sessionId", "ownerUserRef"}
	case "page.open":
		fields = []string{"pageId"}
	}
	for _, field := range fields {
		identity, ok := extractString(value, field)
		if !ok || identity == "" || len(identity) > 4096 {
			return fmt.Errorf("%s response missing bounded nonempty %s", command, field)
		}
	}
	return nil
}

func waitForHealth(ctx context.Context, url string, maxWait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	var lastErr error
	for {
		req, err := newGetRequest(ctx, url)
		if err != nil {
			return err
		}
		resp, err := defaultClient.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 3))
			if err := errors.Join(readErr, resp.Body.Close()); err != nil {
				return fmt.Errorf("health response read/close: %w", err)
			}
			if resp.StatusCode == http.StatusOK && string(body) == "ok" {
				return nil
			}
			lastErr = fmt.Errorf("health response has status %d or invalid bounded body", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return errors.Join(fmt.Errorf("owned serve health did not become ready"), ctx.Err(), lastErr)
		case <-time.After(200 * time.Millisecond):
		}
	}
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
