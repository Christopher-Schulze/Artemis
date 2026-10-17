package fixture

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/router"
)

// RunHybridScenario retains the existing router request, real output and
// transferred resource lifetime. Route failures are never converted to pages.
func RunHybridScenario(ctx context.Context, runner *router.HybridRouter, native *bridge.Page, scenario Scenario, server *Server) (result CrossResult, runErr error) {
	routed, err := runner.Execute(ctx, router.RouteRequest{
		URL: server.URL(scenario.Path), Signals: hybridSignals(scenario), Action: router.ActionFetch,
		Policy: router.Policy{}, State: router.BrowserState{},
	})
	defer func() { runErr = errors.Join(runErr, routed.Close()) }()
	links := make([]string, 0, len(routed.Output.Links))
	for _, link := range routed.Output.Links {
		links = append(links, link.Href)
	}
	result = CrossResult{URL: routed.Output.URL, StatusCode: routed.Output.StatusCode, Title: routed.Output.Title,
		Text: routed.Output.Text, HTML: routed.Output.HTML, Markdown: routed.Output.Markdown, Links: links}
	if err != nil || scenario.Expect.Eval == "" {
		return result, err
	}
	if page, ok := routed.Resource.(*engine.Page); ok {
		return ObserveEnginePage(ctx, page, scenario), nil
	}
	if native == nil || (routed.Evidence.FinalMode != router.ModeChromiumCDP && routed.Evidence.FinalMode != router.ModeStealth) {
		return result, fmt.Errorf("hybrid evaluation requires its actual routed page")
	}
	if err := waitNativeReady(ctx, native); err != nil {
		return result, err
	}
	observed, err := observeNativePage(ctx, native, scenario)
	if err != nil {
		return result, err
	}
	result.EvalResult, result.EvalErr = observed.EvalResult, observed.EvalErr
	return result, nil
}

func hybridSignals(scenario Scenario) bridge.RouterSignals {
	signals := bridge.RouterSignals{IsHTML: true, ScriptCount: scenario.ScriptCount}
	if signals.ScriptCount == 0 && scenario.RunScripts {
		signals.ScriptCount = 1
	}
	signals.HasExternalScripts = signals.ScriptCount > 0 && strings.Contains(scenario.HTML, "<script src=")
	switch scenario.Kind {
	case KindCanvas:
		signals.NeedsCanvas = true
	case KindShadowDOM:
		signals.HasShadowDOM = true
	case KindFrame, KindDialog:
		signals.NeedsLayout = true
	case KindWebSocket, KindServiceWorker:
		signals.HasWebComponents = true
	}
	return signals
}
