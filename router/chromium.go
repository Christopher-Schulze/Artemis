package router

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/observe"
)

// ChromiumPage is the minimal owned CDP page surface required by the hybrid
// router. *bridge.Page satisfies this interface without coupling the router
// to a particular profile/session owner.
type ChromiumPage interface {
	Navigate(context.Context, string) (string, string, error)
	Call(context.Context, string, any, any) error
}

// ChromiumExecutor performs a real CDP navigation and reads the committed
// DOM through Runtime.evaluate. A caller owns page lifecycle and profile
// isolation; this executor owns only the operation.
type ChromiumExecutor struct {
	Page     ChromiumPage
	Prepare  func(context.Context, ExecutionRequest) error
	Observer ObservationProvider
}

type evaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
	AwaitPromise  bool   `json:"awaitPromise"`
}

type evaluateResponse struct {
	ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
	Result           struct {
		Type  string             `json:"type"`
		Value chromiumPageOutput `json:"value"`
	} `json:"result"`
}

type chromiumPageOutput struct {
	URL      string       `json:"url"`
	Title    string       `json:"title"`
	HTML     string       `json:"html"`
	Text     string       `json:"text"`
	Markdown string       `json:"markdown"`
	Links    []agent.Link `json:"links"`
}

func (e ChromiumExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	if e.Page == nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_unavailable", Retryable: false, Cause: fmt.Errorf("CDP page is nil")}
	}
	if e.Prepare != nil {
		if err := e.Prepare(ctx, request); err != nil {
			return ExecutionOutput{}, &RouteFailure{Reason: "chromium_prepare_failed", Retryable: false, Cause: err}
		}
	}
	if _, _, err := e.Page.Navigate(ctx, request.URL); err != nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_navigation_failed", Retryable: true, Cause: err}
	}
	var response evaluateResponse
	if err := e.Page.Call(ctx, "Runtime.evaluate", evaluateParams{
		Expression:    chromiumSnapshotExpression,
		ReturnByValue: true,
		AwaitPromise:  true,
	}, &response); err != nil {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_snapshot_failed", Retryable: true, Cause: err}
	}
	if len(response.ExceptionDetails) > 0 && string(response.ExceptionDetails) != "null" {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_snapshot_exception", Retryable: false, Cause: fmt.Errorf("page snapshot evaluation failed")}
	}
	if response.Result.Type != "object" || response.Result.Value.HTML == "" {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_empty_snapshot", Retryable: false, Cause: fmt.Errorf("page snapshot did not contain HTML")}
	}
	pageURL := response.Result.Value.URL
	if pageURL == "" {
		pageURL = request.URL
	}
	var observation *observe.ObservationEvidence
	if e.Observer != nil {
		evidence, err := e.Observer.CaptureEvidence(ctx)
		if err != nil {
			return ExecutionOutput{}, &RouteFailure{Reason: "chromium_observation_failed", Retryable: false, Cause: err}
		}
		observation = &evidence
	}
	return ExecutionOutput{
		Page: PageOutput{
			URL: pageURL, StatusCode: 200, HTML: response.Result.Value.HTML,
			Text: response.Result.Value.Text, Markdown: response.Result.Value.Markdown,
			Title: response.Result.Value.Title, Links: response.Result.Value.Links,
		},
		State: request.State.Clone(), Observation: observation, Quality: ResultQualityVerified, Verified: true,
	}, nil
}

const chromiumSnapshotExpression = `(async () => {
  if (document.readyState === "loading") {
    await new Promise((resolve) => document.addEventListener("DOMContentLoaded", resolve, {once: true}));
  }
  const root = document.documentElement;
  const body = document.body;
  const links = Array.from(document.querySelectorAll("a[href]")).map((a) => ({
    href: a.href,
    text: (a.innerText || a.textContent || "").trim(),
    title: a.getAttribute("title") || ""
  }));
  return {
    url: location.href,
    title: document.title,
    html: root ? root.outerHTML : "",
    text: body ? (body.innerText || body.textContent || "") : "",
    markdown: "",
    links
  };
})()`
