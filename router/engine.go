package router

import (
	"bytes"
	"context"
	"fmt"
	"net/url"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
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
}

func (e RuntimeExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	if e.Runtime == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_unavailable", Retryable: true, Cause: fmt.Errorf("runtime is nil")}
	}
	page, err := e.Runtime.Fetch(ctx, request.URL, engine.FetchOpts{
		Method: request.Method, Body: bytes.Clone(request.Body), Headers: request.Headers.Clone(),
		RunScripts: e.RunScripts || request.Signals.ScriptCount > 0 || request.Signals.HasExternalScripts,
	})
	if err != nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_execution_failed", Retryable: true, Cause: err}
	}
	if page == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_empty_result", Retryable: true, Cause: fmt.Errorf("runtime returned no page")}
	}
	return outputFromPage(page, request.State), nil
}

func (e RenderlessExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	if e.Engine == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_unavailable", Retryable: true, Cause: fmt.Errorf("engine is nil")}
	}
	page, err := e.Engine.Fetch(ctx, request.URL, engine.FetchOpts{
		Method: request.Method, Body: bytes.Clone(request.Body), Headers: request.Headers.Clone(),
		RunScripts: e.RunScripts || request.Signals.ScriptCount > 0 || request.Signals.HasExternalScripts,
		Console:    e.Console,
	})
	if err != nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_execution_failed", Retryable: true, Cause: err}
	}
	if page == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "renderless_empty_result", Retryable: true, Cause: fmt.Errorf("engine returned no page")}
	}
	state := request.State.Clone()
	state.URL = page.URL()
	if jar := e.Engine.HTTPClient().CookieJar(); jar != nil {
		parsed, parseErr := url.Parse(page.URL())
		if parseErr == nil {
			state.Cookies = jar.Cookies(parsed)
		}
	}
	output := outputFromPage(page, state)
	output.State = state
	return output, nil
}

func outputFromPage(page *engine.Page, state BrowserState) ExecutionOutput {
	return ExecutionOutput{
		Page: PageOutput{
			URL: page.URL(), StatusCode: page.StatusCode(), Headers: page.Headers().Clone(),
			HTML: page.HTML(), Text: page.Text(), Markdown: page.Markdown(), Title: page.Title(), Links: page.Links(),
		},
		State: state.Clone(), Resource: page, Quality: ResultQualityVerified, Verified: true,
	}
}
