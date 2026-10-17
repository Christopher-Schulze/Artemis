package replay

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/network"
)

// Result is actual inert-snapshot evidence, not original-handler execution or
// source authenticity. Passing includes the existing page/engine cleanup.
type Result struct {
	CaptureID  string
	AssetHash  string
	StatusCode int
	Title      string
	Text       string
	URL        string
	Passed     bool
	DurationMs int64
}

// Run uses the existing projection, server, engine and semantic validator.
// It owns a fresh engine, allows only its own HTTP origin and executes no scripts.
func Run(ctx context.Context, content []byte, manifest fixtureasset.Manifest, policy Policy) (result Result, returnErr error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("replay execution: owning context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	started := time.Now()
	defer func() {
		returnErr = errors.Join(returnErr, ctx.Err())
		result.Passed = returnErr == nil
		result.DurationMs = time.Since(started).Milliseconds()
	}()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	record, err := admitAsset(content, manifest, policy)
	if err != nil {
		return Result{}, err
	}
	return runSnapshot(ctx, record, manifest.AssetHash)
}

func runSnapshot(ctx context.Context, record *fixture.Replay, hash string) (result Result, returnErr error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	scenario, err := record.AsRegression()
	if err != nil {
		return Result{}, err
	}
	server := fixture.NewServer()
	defer server.Close()
	accessToken := rand.Text()
	if err := server.RequireAccessToken(accessToken); err != nil {
		return Result{}, err
	}
	if err := server.Register(scenario); err != nil {
		return Result{}, err
	}
	runner, err := newSnapshotEngine(server.BaseURL())
	if err != nil {
		return Result{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, runner.Close()) }()
	page, err := runner.Fetch(ctx, server.URL(scenario.Path), engine.FetchOpts{Headers: http.Header{fixture.AccessTokenHeader: {accessToken}}})
	if err != nil {
		return Result{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, page.Close()) }()
	observed, err := observeSnapshot(page)
	if err != nil {
		return Result{}, err
	}
	result = Result{CaptureID: record.ID, AssetHash: hash, StatusCode: observed.StatusCode, Title: observed.Title, Text: observed.Text, URL: observed.URL}
	return result, fixture.ValidateCrossResult(scenario, observed)
}

func newSnapshotEngine(rawURL string) (*engine.Engine, error) {
	policy, err := snapshotNetworkPolicy(rawURL)
	if err != nil {
		return nil, err
	}
	return engine.New(engine.Config{PolicyConfig: policy, Timeout: 10 * time.Second, MaxBodyBytes: 8 << 20, MaxDownloadDiskBytes: 1,
		SessionBudget: engine.SessionBudget{MaxTabs: 1, MaxRequests: 1, MaxConcurrency: 1, MaxResponseBytes: 8 << 20, MaxDiskBytes: 1, Timeout: 10 * time.Second}})
}

func observeSnapshot(page *engine.Page) (fixture.CrossResult, error) {
	mediaType, _, err := mime.ParseMediaType(page.Headers().Get("Content-Type"))
	if err != nil {
		return fixture.CrossResult{}, err
	}
	observed := fixture.CrossResult{URL: page.URL(), StatusCode: page.StatusCode()}
	switch mediaType {
	case "text/plain":
		observed.Text = string(page.RawBody())
	case "text/html":
		observed.Title, observed.Text = page.Title(), page.Text()
	default:
		return fixture.CrossResult{}, fmt.Errorf("replay execution: unexpected snapshot MIME %q", mediaType)
	}
	return observed, nil
}

func snapshotNetworkPolicy(rawURL string) (network.PolicyConfig, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return network.PolicyConfig{}, err
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return network.PolicyConfig{}, err
	}
	return network.PolicyConfig{AllowedDomains: []string{parsed.Hostname()}, AllowedPorts: []int{port}, AllowedSchemes: []string{"http"},
		AllowedMethods: []string{"GET"}, AllowPrivateNetworks: true, MaxResponseBodyBytes: 8 << 20}, nil
}
