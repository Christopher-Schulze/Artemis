package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/webapi"
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
	URL        string       `json:"url"`
	Title      string       `json:"title"`
	HTML       string       `json:"html"`
	Text       string       `json:"text"`
	Markdown   string       `json:"markdown"`
	Links      []agent.Link `json:"links"`
	StatusCode int          `json:"statusCode"`
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
	return e.Read(ctx, request.State)
}

// Read extracts the committed live DOM without navigating or replacing page identity.
func (e ChromiumExecutor) Read(ctx context.Context, state BrowserState) (ExecutionOutput, error) {
	if e.Page == nil {
		return ExecutionOutput{}, fmt.Errorf("chromium page required")
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
		return ExecutionOutput{}, fmt.Errorf("chromium snapshot omitted document URL")
	}
	root, err := html.Parse(strings.NewReader(response.Result.Value.HTML))
	if err != nil {
		return ExecutionOutput{}, fmt.Errorf("parse live DOM: %w", err)
	}
	doc := webapi.NewDocument(root, pageURL)
	status := response.Result.Value.StatusCode
	var observation *observe.ObservationEvidence
	if e.Observer != nil {
		evidence, err := e.Observer.CaptureEvidence(ctx)
		if err != nil {
			return ExecutionOutput{}, &RouteFailure{Reason: "chromium_observation_failed", Retryable: false, Cause: err}
		}
		observation = &evidence
		for i := len(evidence.Network) - 1; i >= 0; i-- {
			event := evidence.Network[i]
			if event.ResourceType == "Document" && event.URL == pageURL && event.Status >= 100 && event.Status <= 599 {
				status = event.Status
				break
			}
		}
	}
	if status < 100 || status > 599 {
		return ExecutionOutput{}, &RouteFailure{Reason: "chromium_status_unobserved", Retryable: false, Cause: fmt.Errorf("no actual HTTP document status observed")}
	}
	return ExecutionOutput{
		Page: PageOutput{
			URL: pageURL, StatusCode: status, HTML: response.Result.Value.HTML,
			Text: response.Result.Value.Text, Markdown: agent.Markdown(doc),
			Title: response.Result.Value.Title, Links: agent.Links(doc),
		},
		State: state.Clone(), Observation: observation, Quality: ResultQualityObserved, Verified: true,
	}, nil
}

const chromiumSnapshotExpression = `(async () => {
  if (document.readyState === "loading") {
    await new Promise((resolve) => document.addEventListener("DOMContentLoaded", resolve, {once: true}));
  }
  const root = document.documentElement;
  const body = document.body;
  const html = root ? root.outerHTML : "";
  const text = body ? (body.innerText || body.textContent || "") : "";
  if (html.length > 1048576 || text.length > 1048576) throw new Error("live DOM exceeds extraction limit");
  return {
    url: location.href,
    title: document.title,
    html, text,
    statusCode: performance.getEntriesByType("navigation")[0]?.responseStatus || 0
  };
})()`
