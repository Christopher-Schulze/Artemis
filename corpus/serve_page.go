package corpus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func runServePage(ctx context.Context, client *serve.Client, sessionID string, manifest fixtureasset.Manifest, scenario fixture.Scenario, server *fixture.Server) (result Result, runErr error) {
	var opened serve.PageOpenResult
	if err := client.Call(ctx, serve.CmdPageOpen, serve.PageOpenParams{SessionID: sessionID,
		URL: server.URL(scenario.Path), RunScripts: scenario.RunScripts, AsyncFetch: scenario.AsyncFetch}, &opened); err != nil {
		return result, err
	}
	if opened.PageID == "" || opened.URL == "" || opened.Status < 100 || opened.Status > 599 {
		return result, fmt.Errorf("fixture Serve page omitted actual identity/status")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, client.Call(cleanup, serve.CmdPageClose, serve.PageCloseParams{SessionID: sessionID, PageID: opened.PageID}, &serve.EmptyResult{}))
	}()
	if scenario.RunScripts && scenario.WaitForIdle {
		if err := client.Call(ctx, serve.CmdPageWaitIdle, serve.PageWaitIdleParams{SessionID: sessionID, PageID: opened.PageID}, &serve.EmptyResult{}); err != nil {
			return result, err
		}
	}
	observed, err := observeServePage(ctx, client, sessionID, opened, scenario)
	result = Result{FixtureManifest: manifest.Detached(), StatusCode: observed.StatusCode, URL: observed.URL,
		Title: observed.Title, Text: observed.Text, HTML: observed.HTML, Markdown: observed.Markdown,
		EvalResult: observed.EvalResult, EvalError: observed.EvalErr, Links: observed.Links}
	if err != nil {
		return result, err
	}
	return result, fixture.ValidateCrossResult(scenario, observed)
}

func observeServePage(ctx context.Context, client *serve.Client, sessionID string, opened serve.PageOpenResult, scenario fixture.Scenario) (result fixture.CrossResult, err error) {
	result.URL, result.StatusCode = opened.URL, opened.Status
	if scenario.Expect.Eval != "" {
		var evaluation serve.PageEvalResult
		if err := client.Call(ctx, serve.CmdPageEval, serve.PageEvalParams{SessionID: sessionID, PageID: opened.PageID, Expr: scenario.Expect.Eval}, &evaluation); err != nil {
			result.EvalErr = err.Error()
		} else {
			result.EvalResult = evaluation.Value
		}
	}
	for _, dump := range []struct {
		format serve.DumpFormat
		target *string
	}{{serve.DumpText, &result.Text}, {serve.DumpTitle, &result.Title}, {serve.DumpHTML, &result.HTML}, {serve.DumpMarkdown, &result.Markdown}} {
		var value struct {
			Data string `json:"data"`
		}
		if err := client.Call(ctx, serve.CmdPageDump, serve.PageDumpParams{SessionID: sessionID, PageID: opened.PageID, Format: string(dump.format)}, &value); err != nil {
			return result, err
		}
		*dump.target = value.Data
	}
	var links struct {
		Data []struct{ Href string } `json:"data"`
	}
	if err := client.Call(ctx, serve.CmdPageDump, serve.PageDumpParams{SessionID: sessionID, PageID: opened.PageID, Format: string(serve.DumpLinks)}, &links); err != nil {
		return result, err
	}
	result.Links = make([]string, 0, len(links.Data))
	for _, link := range links.Data {
		result.Links = append(result.Links, link.Href)
	}
	return result, nil
}
