package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeFormCDP struct {
	mu         sync.Mutex
	counts     map[string]int
	failMethod string
	fillStatus string
}

func (f *fakeFormCDP) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[method]++
	if f.failMethod == method {
		f.failMethod = ""
		f.mu.Unlock()
		return fmt.Errorf("forced %s failure", method)
	}
	fillStatus := f.fillStatus
	f.mu.Unlock()
	switch method {
	case "DOM.getDocument":
		result.(*domGetDocumentResult).Root.NodeID = 1
	case "DOM.querySelector":
		request := params.(domQuerySelectorParams)
		nodeID := int64(0)
		switch request.Selector {
		case "#form":
			nodeID = 2
		case "#alpha":
			nodeID = 10
		case "#bravo":
			nodeID = 11
		}
		result.(*domQuerySelectorResult).NodeID = nodeID
	case "Accessibility.queryAXTree":
		output := result.(*accessibilityQueryResult)
		output.Nodes = make([]struct {
			BackendDOMNodeID int64 `json:"backendDOMNodeId"`
			Ignored          bool  `json:"ignored"`
		}, 2)
		output.Nodes[0].BackendDOMNodeID = 100
		output.Nodes[1].BackendDOMNodeID = 101
	case "DOM.describeNode":
		request := params.(domDescribeNodeParams)
		result.(*domDescribeNodeResult).Node.BackendNodeID = request.NodeID + 90
	case "DOM.resolveNode":
		request := params.(domResolveNodeParams)
		result.(*domResolveNodeResult).Object.ObjectID = fmt.Sprintf("object-%d", request.NodeID)
	case "DOM.getBoxModel":
		quad := []float64{0, 0, 10, 0, 10, 10, 0, 10}
		model := &result.(*domGetBoxModelResult).Model
		model.Content = append([]float64(nil), quad...)
		model.Padding = append([]float64(nil), quad...)
		model.Border = append([]float64(nil), quad...)
		model.Margin = append([]float64(nil), quad...)
		model.Width, model.Height = 10, 10
	case "Runtime.evaluate":
		request := params.(runtimeEvaluateParams)
		value := json.RawMessage("true")
		if strings.Contains(request.Expression, "new MutationObserver") {
			value = json.RawMessage(`{"ok":true,"epoch":0}`)
		}
		result.(*runtimeEvaluateResult).Result.Value = value
	case "Runtime.callFunctionOn":
		if fillStatus == "" {
			fillStatus = "filled"
		}
		encoded, err := json.Marshal(fillStatus)
		if err != nil {
			return err
		}
		result.(*runtimeCallFunctionResult).Result.Value = encoded
	}
	return nil
}

func (f *fakeFormCDP) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[method]
}

type fakeFormEventStream struct {
	events chan FormCDPEvent
	errors chan error
	done   chan struct{}
	once   sync.Once
}

func newFakeFormEventStream() *fakeFormEventStream {
	return &fakeFormEventStream{events: make(chan FormCDPEvent, 8), errors: make(chan error, 1), done: make(chan struct{})}
}

func (s *fakeFormEventStream) Next(ctx context.Context) (FormCDPEvent, error) {
	select {
	case event := <-s.events:
		return event, nil
	case err := <-s.errors:
		return FormCDPEvent{}, err
	case <-s.done:
		return FormCDPEvent{}, errors.New("stream closed")
	case <-ctx.Done():
		return FormCDPEvent{}, ctx.Err()
	}
}

func (s *fakeFormEventStream) Close() {
	s.once.Do(func() { close(s.done) })
}

func newTestCDPFormRuntime(t *testing.T, caller *fakeFormCDP, stream *fakeFormEventStream) *FormIntentRuntime {
	t.Helper()
	runtime, err := NewCDPFormIntentRuntime(CDPFormIntentConfig{
		Caller: caller, SessionID: "session", PageID: "page",
		Subscribe: func(int) (FormCDPEventStream, error) { return stream, nil },
	}, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func cdpTestIntent() FormIntent {
	return FormIntent{
		SessionID: "session", PageID: "page", FormRoot: "#form",
		Fields: []FormField{
			{Name: "alpha", Selector: "#alpha", Value: "secret-one"},
			{Name: "bravo", Selector: "#bravo", Value: "secret-two"},
		},
	}
}

func TestCDPFormIntentBatchesPrefetchCachesAndInvalidatesOnMutation(t *testing.T) {
	caller := &fakeFormCDP{}
	stream := newFakeFormEventStream()
	runtime := newTestCDPFormRuntime(t, caller, stream)
	intent := cdpTestIntent()
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	intent.Fields[0].Value = "new-secret"
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if caller.count("Accessibility.queryAXTree") != 1 || caller.count("DOM.getBoxModel") != 2 || caller.count("Runtime.callFunctionOn") != 4 {
		t.Fatalf("unexpected CDP counts: AX=%d box=%d fill=%d", caller.count("Accessibility.queryAXTree"), caller.count("DOM.getBoxModel"), caller.count("Runtime.callFunctionOn"))
	}
	payload, err := json.Marshal(runtimeBindingCalled{Name: formBindingName("session", "page"), Payload: formMutationToken(intent.Identity(), 1)})
	if err != nil {
		t.Fatal(err)
	}
	stream.events <- FormCDPEvent{Method: "Runtime.bindingCalled", Params: payload, SessionID: "session"}
	waitForFormMetric(t, runtime, func(metrics FormIntentMetrics) bool { return metrics.CacheInvalidationsTotal == 1 })
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if caller.count("Accessibility.queryAXTree") != 2 || caller.count("DOM.getBoxModel") != 4 {
		t.Fatalf("mutation did not refetch: AX=%d box=%d", caller.count("Accessibility.queryAXTree"), caller.count("DOM.getBoxModel"))
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if caller.count("Runtime.addBinding") != 1 || caller.count("Runtime.removeBinding") != 1 {
		t.Fatalf("binding lifecycle counts: add=%d remove=%d", caller.count("Runtime.addBinding"), caller.count("Runtime.removeBinding"))
	}
}

func TestCDPFormIntentPartialPrefetchFailureReleasesAndDoesNotCache(t *testing.T) {
	caller := &fakeFormCDP{failMethod: "DOM.getBoxModel"}
	stream := newFakeFormEventStream()
	runtime := newTestCDPFormRuntime(t, caller, stream)
	if _, err := runtime.Execute(context.Background(), cdpTestIntent()); err == nil {
		t.Fatal("partial box-model failure accepted")
	}
	if caller.count("Runtime.releaseObjectGroup") == 0 || caller.count("Runtime.callFunctionOn") != 0 {
		t.Fatalf("partial state retained: releases=%d fills=%d", caller.count("Runtime.releaseObjectGroup"), caller.count("Runtime.callFunctionOn"))
	}
	if _, err := runtime.Execute(context.Background(), cdpTestIntent()); err != nil {
		t.Fatal(err)
	}
	if caller.count("Accessibility.queryAXTree") != 2 {
		t.Fatalf("prefetch count = %d, want 2", caller.count("Accessibility.queryAXTree"))
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCDPFormIntentRejectsCrossPageAndFailsClosedOnStreamError(t *testing.T) {
	caller := &fakeFormCDP{}
	stream := newFakeFormEventStream()
	runtime := newTestCDPFormRuntime(t, caller, stream)
	intent := cdpTestIntent()
	crossPage := intent
	crossPage.PageID = "other-page"
	if _, err := runtime.Execute(context.Background(), crossPage); err == nil {
		t.Fatal("cross-page intent accepted")
	}
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	stream.errors <- errors.New("transport failed")
	waitForFormMetric(t, runtime, func(metrics FormIntentMetrics) bool { return metrics.CacheInvalidationsTotal == 1 })
	if _, err := runtime.Execute(context.Background(), intent); err == nil || !strings.Contains(err.Error(), "mutation stream") {
		t.Fatalf("error = %v, want terminal mutation stream error", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCDPFormIntentStaleNodeRetriesThenLeavesNoCache(t *testing.T) {
	caller := &fakeFormCDP{fillStatus: "stale"}
	stream := newFakeFormEventStream()
	runtime := newTestCDPFormRuntime(t, caller, stream)
	if _, err := runtime.Execute(context.Background(), cdpTestIntent()); !errors.Is(err, ErrFormIntentStale) {
		t.Fatalf("error = %v, want stale", err)
	}
	if caller.count("Accessibility.queryAXTree") != 2 || caller.count("Runtime.releaseObjectGroup") != 2 {
		t.Fatalf("stale lifecycle: AX=%d releases=%d", caller.count("Accessibility.queryAXTree"), caller.count("Runtime.releaseObjectGroup"))
	}
	caller.mu.Lock()
	caller.fillStatus = "filled"
	caller.mu.Unlock()
	if _, err := runtime.Execute(context.Background(), cdpTestIntent()); err != nil {
		t.Fatal(err)
	}
	if caller.count("Accessibility.queryAXTree") != 3 {
		t.Fatalf("stale cache retained: AX=%d", caller.count("Accessibility.queryAXTree"))
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForFormMetric(t *testing.T, runtime *FormIntentRuntime, ready func(FormIntentMetrics) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready(runtime.Metrics()) {
		if time.Now().After(deadline) {
			t.Fatalf("metrics did not converge: %+v", runtime.Metrics())
		}
		time.Sleep(time.Millisecond)
	}
}
