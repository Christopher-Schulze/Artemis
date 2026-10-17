package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
	"github.com/Christopher-Schulze/Artemis/parser"
)

type competitorSession struct {
	transport *bridge.CDPTransport
	id        string
}

func (s competitorSession) Call(ctx context.Context, method string, params, result any) error {
	return s.transport.CallSession(ctx, s.id, method, params, result)
}

func (r *CompetitorRunner) runCDPScenario(ctx context.Context, scenario Scenario, targetURL string) (evidence ValidationEvidence, returnErr error) {
	if ctx == nil {
		return evidence, fmt.Errorf("competitor scenario context required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var browserContext struct {
		ID string `json:"browserContextId"`
	}
	if err := r.transport.Call(ctx, "Target.createBrowserContext", struct{}{}, &browserContext); err != nil {
		return evidence, err
	}
	if browserContext.ID == "" {
		return evidence, fmt.Errorf("competitor returned empty browser context")
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelCleanup()
		params := struct {
			ID string `json:"browserContextId"`
		}{browserContext.ID}
		returnErr = errors.Join(returnErr, r.transport.Call(cleanup, "Target.disposeBrowserContext", params, nil))
	}()
	var target struct {
		ID string `json:"targetId"`
	}
	create := struct {
		URL       string `json:"url"`
		ContextID string `json:"browserContextId"`
	}{"about:blank", browserContext.ID}
	if err := r.transport.Call(ctx, "Target.createTarget", create, &target); err != nil {
		return evidence, err
	}
	if target.ID == "" {
		return evidence, fmt.Errorf("competitor returned empty target")
	}
	var attached struct {
		ID string `json:"sessionId"`
	}
	attach := struct {
		ID      string `json:"targetId"`
		Flatten bool   `json:"flatten"`
	}{target.ID, true}
	if err := r.transport.Call(ctx, "Target.attachToTarget", attach, &attached); err != nil {
		return evidence, err
	}
	if attached.ID == "" {
		return evidence, fmt.Errorf("competitor returned empty session")
	}
	caller := competitorSession{transport: r.transport, id: attached.ID}
	navigator := cdpops.NewNavigator(caller)
	navigation := navigator.Navigate(ctx, cdpops.NavigationRequest{URL: targetURL, WaitUntil: cdpops.WaitLoad, Timeout: 10 * time.Second})
	if !navigation.Success {
		return evidence, fmt.Errorf("competitor navigation: %s", navigation.Error)
	}
	params := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{"document.documentElement.outerHTML", true}
	var evaluated struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := caller.Call(ctx, "Runtime.evaluate", params, &evaluated); err != nil {
		return evidence, err
	}
	if len(evaluated.Exception) > 0 || evaluated.Result.Type != "string" || evaluated.Result.Value == "" {
		return evidence, fmt.Errorf("competitor DOM extraction failed")
	}
	document, err := parser.ParseHTML(strings.NewReader(evaluated.Result.Value), targetURL)
	if err != nil {
		return evidence, fmt.Errorf("competitor DOM parse: %w", err)
	}
	return validateDocument(scenario, document), nil
}
