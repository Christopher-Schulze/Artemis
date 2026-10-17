package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
)

// RenderlessExecutor adapts the real V8/renderless engine to the canonical
// router contract. It never reports success without a fetched page.
type RenderlessExecutor struct {
	Engine     *engine.Engine
	RunScripts bool
	Console    js.Console
}

// FetchRuntime is the lifecycle-safe subset required by the Agent runtime.
type FetchRuntime interface {
	Fetch(context.Context, string, engine.FetchOpts) (*engine.Page, error)
}

// RuntimeExecutor adapts an Agent-owned runtime without taking lifecycle
// ownership of it.
type RuntimeExecutor struct {
	Runtime    FetchRuntime
	RunScripts bool
	// AsyncFetch enables concurrent fetches and waits for their settled output
	// before returning the owned page to the router.
	AsyncFetch bool
}

func (e RuntimeExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	ctx = network.WithSessionID(ctx, request.State.SessionID)
	if e.Runtime == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_unavailable", Retryable: true, Cause: fmt.Errorf("runtime is nil")}
	}
	page, err := e.Runtime.Fetch(ctx, request.URL, engine.FetchOpts{
		Method: request.Method, Body: bytes.Clone(request.Body), Headers: request.Headers.Clone(),
		RunScripts: e.RunScripts || request.Signals.ScriptCount > 0 || request.Signals.HasExternalScripts,
		AsyncFetch: e.AsyncFetch || request.AsyncFetch,
	})
	if err != nil {
		return ExecutionOutput{}, renderlessFetchFailure(request, err)
	}
	if page == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_empty_result", Retryable: true, Cause: fmt.Errorf("runtime returned no page")}
	}
	if e.AsyncFetch || request.AsyncFetch {
		if err := page.WaitIdle(ctx); err != nil {
			return ExecutionOutput{}, renderlessIdleFailure(request, errors.Join(err, page.Close()))
		}
	}
	state := request.State.Clone()
	if provider, ok := e.Runtime.(interface{ HTTPClient() *network.HTTPClient }); ok {
		jar, err := provider.HTTPClient().CookieJarForSession(request.State.SessionID)
		if err != nil {
			return ExecutionOutput{}, errors.Join(err, page.Close())
		}
		parsed, err := url.Parse(page.URL())
		if err != nil {
			return ExecutionOutput{}, errors.Join(err, page.Close())
		}
		state.Cookies = jar.Cookies(parsed)
	}
	return outputFromPage(page, state), nil
}

func (e RenderlessExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	ctx = network.WithSessionID(ctx, request.State.SessionID)
	if e.Engine == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_unavailable", Retryable: true, Cause: fmt.Errorf("engine is nil")}
	}
	page, err := e.Engine.Fetch(ctx, request.URL, engine.FetchOpts{
		Method: request.Method, Body: bytes.Clone(request.Body), Headers: request.Headers.Clone(),
		RunScripts: e.RunScripts || request.Signals.ScriptCount > 0 || request.Signals.HasExternalScripts,
		Console:    e.Console,
		AsyncFetch: request.AsyncFetch,
	})
	if err != nil {
		return ExecutionOutput{}, renderlessFetchFailure(request, err)
	}
	if page == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_empty_result", Retryable: true, Cause: fmt.Errorf("engine returned no page")}
	}
	if request.AsyncFetch {
		if err := page.WaitIdle(ctx); err != nil {
			return ExecutionOutput{}, renderlessIdleFailure(request, errors.Join(err, page.Close()))
		}
	}
	state := request.State.Clone()
	state.URL = page.URL()
	jar, jarErr := e.Engine.HTTPClient().CookieJarForSession(network.SessionID(ctx, e.Engine.Config().SessionID))
	if jarErr != nil {
		return ExecutionOutput{}, errors.Join(jarErr, page.Close())
	}
	if jar != nil {
		parsed, parseErr := url.Parse(page.URL())
		if parseErr == nil {
			state.Cookies = jar.Cookies(parsed)
		}
	}
	output := outputFromPage(page, state)
	output.State = state
	return output, nil
}

func renderlessFetchFailure(request ExecutionRequest, err error) *RouteFailure {
	retryable := true
	var execution *css.ExecutionFailure
	if errors.As(err, &execution) {
		retryable = false
	}
	if css.IsDiagnostic(err) {
		method := strings.ToUpper(request.Method)
		retryable = retryable && !engine.IsBudgetExceeded(err) && !css.IsBudgetFailure(err) && len(request.Body) == 0 && (method == "" || method == http.MethodGet || method == http.MethodHead)
	}
	return &RouteFailure{Reason: "renderless_execution_failed", Retryable: retryable, Cause: err}
}

func renderlessIdleFailure(request ExecutionRequest, err error) *RouteFailure {
	if css.IsDiagnostic(err) {
		err = &css.ExecutionFailure{Cause: err}
	}
	failure := renderlessFetchFailure(request, err)
	failure.Reason = "renderless_idle_failed"
	return failure
}

func outputFromPage(page *engine.Page, state BrowserState) ExecutionOutput {
	state = state.Clone()
	state.URL = page.URL()
	if ctx := page.JSContext(); ctx != nil {
		state.LocalStorage, state.SessionStorage = ctx.StorageSnapshot()
	}
	return ExecutionOutput{
		Page: PageOutput{
			URL: page.URL(), StatusCode: page.StatusCode(), Headers: page.Headers().Clone(),
			HTML: page.HTML(), Text: page.Text(), Markdown: page.Markdown(), Title: page.Title(), Links: page.Links(),
			Degradations: page.Degradations(),
		},
		State: state.Clone(), Resource: page, Quality: ResultQualityVerified, Verified: true,
	}
}
