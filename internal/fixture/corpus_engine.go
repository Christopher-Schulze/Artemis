package fixture

import (
	"context"
	"errors"

	"github.com/Christopher-Schulze/Artemis/engine"
)

// RunEngineScenario is the shared actual renderless execution path for corpus
// consumers. Its owner supplies the admitted scenario and existing engine.
func RunEngineScenario(ctx context.Context, runner *engine.Engine, scenario Scenario, server *Server) (result CrossResult, runErr error) {
	page, err := runner.Fetch(ctx, server.URL(scenario.Path), engine.FetchOpts{RunScripts: scenario.RunScripts, AsyncFetch: scenario.AsyncFetch})
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, page.Close()) }()
	if scenario.RunScripts && scenario.WaitForIdle {
		if err := page.WaitIdle(ctx); err != nil {
			return result, err
		}
	}
	return ObserveEnginePage(ctx, page, scenario), nil
}

// ObserveEnginePage preserves the existing full cross-adapter observation.
// Evaluation errors remain explicit evidence for the semantic validator.
func ObserveEnginePage(ctx context.Context, page *engine.Page, scenario Scenario) CrossResult {
	var evaluation, evaluationError string
	if scenario.Expect.Eval != "" {
		value, err := page.Eval(ctx, scenario.Expect.Eval)
		if err != nil {
			evaluationError = err.Error()
		} else {
			evaluation = value.String()
			value.Release()
		}
	}
	pageLinks := page.Links()
	links := make([]string, 0, len(pageLinks))
	for _, link := range pageLinks {
		links = append(links, link.Href)
	}
	return CrossResult{URL: page.URL(), StatusCode: page.StatusCode(), Title: page.Title(), Text: page.Text(),
		HTML: page.HTML(), Markdown: page.Markdown(), EvalResult: evaluation, EvalErr: evaluationError, Links: links}
}
