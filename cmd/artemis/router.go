package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

func routePage(ctx context.Context, eng *engine.Engine, targetURL string, runScripts bool, headers http.Header, console js.Console, action artemisrouter.Action) (*artemisrouter.RouteResult, error) {
	if eng == nil {
		return nil, fmt.Errorf("router: engine is required")
	}
	executor := artemisrouter.RenderlessExecutor{Engine: eng, Console: console}
	hybrid, err := artemisrouter.New(artemisrouter.Config{Executors: map[artemisrouter.Mode]artemisrouter.Executor{
		artemisrouter.ModeStaticFetch:  executor,
		artemisrouter.ModeRenderlessJS: executor,
	}})
	if err != nil {
		return nil, err
	}
	signals := artemisrouter.Signals{IsHTML: true}
	if runScripts {
		signals.ScriptCount = 1
	}
	result, err := hybrid.Execute(ctx, artemisrouter.RouteRequest{
		URL: targetURL, Headers: headers, Action: action, Signals: signals,
		TraceID: "cli", EvidenceID: "cli",
	})
	if err != nil {
		return nil, err
	}
	if _, ok := result.Resource.(*engine.Page); !ok {
		_ = result.Close()
		return nil, fmt.Errorf("router: no retained renderless page")
	}
	return &result, nil
}
