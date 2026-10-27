package fixture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/telemetry"
)

// RunNativeScenario uses the existing target/frame/loader-bound observer.
// Its event consumer is joined without writing an unused trace archive. Every
// step error names its phase and the time since the scenario started.
func RunNativeScenario(ctx context.Context, page *bridge.Page, scenario Scenario, target string) (result CrossResult, runErr error) {
	started := time.Now()
	recorder, err := telemetry.NewBrowserTraceRecorder(page, telemetry.TraceRecordConfig{DiscardArchive: true})
	if err != nil {
		return result, PhaseFailure(PhaseTraceStart, started, err)
	}
	if err := recorder.Start(ctx); err != nil {
		return result, PhaseFailure(PhaseTraceStart, started, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := recorder.Stop(cleanup)
		runErr = errors.Join(runErr, PhaseFailure(PhaseTraceStop, started, err))
	}()
	frame, loader, err := page.Navigate(ctx, target)
	if err != nil {
		return result, PhaseFailure(PhaseNavigation, started, err)
	}
	if err := waitNativeReady(ctx, page); err != nil {
		return result, PhaseFailure(PhaseReadiness, started, err)
	}
	result, err = observeNativeOutcome(ctx, page, recorder, frame, loader, scenario)
	return result, PhaseFailure(PhaseObservation, started, err)
}

func waitNativeReady(ctx context.Context, page *bridge.Page) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		var response struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &response); err != nil {
			return err
		}
		if response.Result.Value == "complete" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func observeNativeOutcome(ctx context.Context, page *bridge.Page, recorder *telemetry.BrowserTraceRecorder, frame, loader string, scenario Scenario) (CrossResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var pending error
	var result CrossResult
	for {
		response, found, err := recorder.DocumentResponse(frame, loader)
		if err != nil {
			return result, err
		}
		if found {
			result, err = observeNativePage(ctx, page, scenario)
			if err != nil {
				return result, err
			}
			result.StatusCode, result.URL = response.Status, response.URL
			pending = ValidateCrossResult(scenario, result)
			if (!scenario.WaitForIdle && !scenario.AsyncFetch) || pending == nil {
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, errors.Join(ctx.Err(), pending, fmt.Errorf("fixture actual document response/outcome unavailable"))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func observeNativePage(ctx context.Context, page *bridge.Page, scenario Scenario) (CrossResult, error) {
	var snapshot struct {
		Result struct {
			Value CrossResult `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": nativeSnapshotExpression, "returnByValue": true, "awaitPromise": true}, &snapshot); err != nil {
		return CrossResult{}, err
	}
	result := snapshot.Result.Value
	if scenario.Expect.Eval != "" {
		var evaluation struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": scenario.Expect.Eval, "returnByValue": true}, &evaluation); err != nil {
			result.EvalErr = err.Error()
		} else {
			result.EvalResult = evaluation.Result.Value
		}
	}
	return result, nil
}

const nativeSnapshotExpression = `(async () => {
  if (document.readyState === "loading") {
    await new Promise((resolve) => document.addEventListener("DOMContentLoaded", resolve, {once: true}));
  }
  const root = document.documentElement;
  const body = document.body;
  const links = Array.from(document.querySelectorAll("a[href]")).map((a) => a.href);
  return {
    url: location.href,
    title: document.title,
    html: root ? root.outerHTML : "",
    text: body ? (body.innerText || body.textContent || "") : "",
    markdown: "",
    links
  };
})()`
