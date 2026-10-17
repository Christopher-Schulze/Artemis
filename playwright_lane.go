package artemis

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Christopher-Schulze/Artemis/playwright"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

// PlaywrightLaneConfig is the Playwright peer lane's artifact cache and
// signer trust. Artemis admits, caches and projects the artifact; executing
// it requires a host sandboxed runtime, so a provisioned lane still reports
// itself unavailable until one is attached.
type PlaywrightLaneConfig struct {
	CacheDir     string
	TrustedKeys  playwright.TrustedKeys
	MinFreeBytes int64
	PolicyRef    string
}

type playwrightLane struct {
	mu     sync.Mutex
	cache  *playwright.Cache
	status *playwright.Status
}

// PlaywrightLane returns the BrowserToolCapabilityProjection of the
// Playwright lane, verifying the cached last-known-good artifact on first use.
func (a *Agent) PlaywrightLane(ctx context.Context) playwright.Projection {
	status, policyRef := a.playwrightStatus(ctx, false)
	return playwright.Project(status, false, policyRef)
}

// RefreshPlaywrightLane re-verifies the cached artifact, for example after an
// out-of-band install or suspected corruption.
func (a *Agent) RefreshPlaywrightLane(ctx context.Context) playwright.Projection {
	status, policyRef := a.playwrightStatus(ctx, true)
	return playwright.Project(status, false, policyRef)
}

// InstallPlaywrightArtifact admits a signed artifact directory into the lane
// cache; a rejected candidate leaves the last-known-good artifact serving.
func (a *Agent) InstallPlaywrightArtifact(ctx context.Context, dir string) (playwright.Projection, *TaskError) {
	cache, err := a.playwrightCache()
	if err != nil {
		return playwright.Projection{}, newTaskError(TaskErrorCapabilityUnavailable, "playwright_install", err)
	}
	if _, err := cache.Install(ctx, dir); err != nil {
		code := TaskErrorPolicyDenied
		if errors.Is(err, playwright.ErrInsufficientSpace) {
			code = TaskErrorResourceLimit
		}
		return a.RefreshPlaywrightLane(ctx), newTaskError(code, "playwright_install", err)
	}
	return a.RefreshPlaywrightLane(ctx), nil
}

// ResolveBrowserTool resolves an engine intent and requirements through the
// BrowserToolRouter. An existing Artemis session keeps its engine: a
// Playwright decision for it is refused rather than silently redirected.
func (a *Agent) ResolveBrowserTool(ctx context.Context, sessionID string, intent artemisrouter.AgentBrowserIntent, requirements []artemisrouter.ToolRequirement) (artemisrouter.ToolDecision, *TaskError) {
	decision, err := artemisrouter.ResolveTool(intent, requirements, a.PlaywrightLane(ctx))
	if err != nil {
		var unavailable *artemisrouter.ToolUnavailableError
		if errors.As(err, &unavailable) {
			return artemisrouter.ToolDecision{}, newTaskError(TaskErrorCapabilityUnavailable, "resolve_browser_tool", err)
		}
		return artemisrouter.ToolDecision{}, newTaskError(TaskErrorInvalidInput, "resolve_browser_tool", err)
	}
	if sessionID != "" && decision.Engine != artemisrouter.EngineArtemis {
		if _, ok := a.Session(sessionID); ok {
			return artemisrouter.ToolDecision{}, newTaskError(TaskErrorInvalidTransition, "resolve_browser_tool",
				fmt.Errorf("session %s is owned by the artemis engine; open a new %s session", sessionID, decision.Engine))
		}
	}
	return decision, nil
}

func (a *Agent) playwrightStatus(ctx context.Context, refresh bool) (playwright.Status, string) {
	lane := &a.playwrightLane
	policyRef := ""
	if config := a.config.Playwright; config != nil {
		policyRef = config.PolicyRef
	}
	cache, err := a.playwrightCache()
	if err != nil {
		return playwright.Status{Diagnostics: []string{err.Error()}, Remediation: "configure a Playwright artifact cache and trusted signer"}, policyRef
	}
	lane.mu.Lock()
	defer lane.mu.Unlock()
	if lane.status == nil || refresh {
		status := cache.Load(ctx)
		lane.status = &status
	}
	return *lane.status, policyRef
}

func (a *Agent) playwrightCache() (*playwright.Cache, error) {
	lane := &a.playwrightLane
	lane.mu.Lock()
	defer lane.mu.Unlock()
	if lane.cache != nil {
		return lane.cache, nil
	}
	config := a.config.Playwright
	if config == nil {
		return nil, errors.New("playwright lane is not configured")
	}
	cache, err := playwright.OpenCache(config.CacheDir, config.TrustedKeys, config.MinFreeBytes)
	if err != nil {
		return nil, err
	}
	lane.cache = cache
	return cache, nil
}
