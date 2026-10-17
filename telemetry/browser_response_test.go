package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func TestTraceConsumerDrainsClosedEventAndErrorBuffers(t *testing.T) {
	events := make(chan bridge.CDPEvent, traceEventBuffer)
	failures := make(chan error, 1)
	for index := 0; index < cap(events); index++ {
		events <- bridge.CDPEvent{SessionID: "session", Method: "Network.requestWillBeSent", Params: json.RawMessage(fmt.Sprintf(`{"requestId":"%d","frameId":"frame","loaderId":"loader","type":"Document","request":{"url":"https://example.test/","method":"GET"}}`, index))}
	}
	lost := errors.New("explicit event overflow")
	failures <- lost
	close(events)
	close(failures)
	c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	r := &BrowserTraceRecorder{}
	r.workers.Add(1)
	r.consumeEvents(&bridge.CDPSubscription{Events: events, Errors: failures}, c)
	r.workers.Wait()
	if len(c.snapshot().Network) != traceEventBuffer || !errors.Is(c.err(), lost) {
		t.Fatalf("closed subscription discarded queued evidence: entries=%d error=%v", len(c.snapshot().Network), c.err())
	}
}

func TestDocumentResponseRequiresActualResponseURLAndHonorsEmptyFailure(t *testing.T) {
	for _, responseURL := range []string{"", "https://example.test/" + strings.Repeat("x", traceTextLimit), "https://example.test/actual?secret=private"} {
		c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
		r := &BrowserTraceRecorder{active: true, collector: c}
		recordResponseEvent(t, c, "Network.requestWillBeSent", `{"requestId":"root","frameId":"frame","loaderId":"loader","type":"Document","request":{"url":"https://example.test/request-intent","method":"GET"}}`)
		recordResponseEvent(t, c, "Network.responseReceived", fmt.Sprintf(`{"requestId":"root","response":{"url":%q,"status":200}}`, responseURL))
		entry, found, err := r.DocumentResponse("frame", "loader")
		if responseURL == "" || len(responseURL) > traceTextLimit {
			if err == nil || found {
				t.Fatal("missing response URL was replaced with request intent")
			}
			continue
		}
		if err != nil || !found || entry.URL != "https://example.test/actual" {
			t.Fatalf("actual response URL was not retained: %+v found=%v err=%v", entry, found, err)
		}
		recordResponseEvent(t, c, "Network.loadingFailed", `{"requestId":"root","errorText":""}`)
		if _, found, err := r.DocumentResponse("frame", "loader"); err == nil || found {
			t.Fatal("empty loadingFailed reason left successful document evidence")
		}
	}
}

func recordResponseEvent(t *testing.T, collector *traceEventCollector, method, payload string) {
	t.Helper()
	if !json.Valid([]byte(payload)) {
		t.Fatal("invalid protocol test input")
	}
	collector.record(bridge.CDPEvent{SessionID: "session", Method: method, Params: json.RawMessage(payload)})
}

func TestDocumentResponseRequiresExactObservedNavigation(t *testing.T) {
	c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	r := &BrowserTraceRecorder{active: true, collector: c}
	request := `{"requestId":"root","frameId":"root-frame","loaderId":"navigation","type":"Document","request":{"url":"https://example.test/start?secret=private","method":"GET"}}`
	recordResponseEvent(t, c, "Network.requestWillBeSent", request)
	recordResponseEvent(t, c, "Network.loadingFinished", `{"requestId":"root"}`)
	if entry, found, err := r.DocumentResponse("root-frame", "navigation"); err != nil || found || entry.Status != 0 {
		t.Fatalf("request completion manufactured HTTP status: %+v %v %v", entry, found, err)
	}
	for _, ids := range [][2]string{{"child-frame", "navigation"}, {"root-frame", "previous"}} {
		recordResponseEvent(t, c, "Network.requestWillBeSent", fmt.Sprintf(`{"requestId":%q,"frameId":%q,"loaderId":%q,"type":"Document","request":{"url":"https://example.test/final","method":"GET"}}`, ids[0]+ids[1], ids[0], ids[1]))
		recordResponseEvent(t, c, "Network.responseReceived", fmt.Sprintf(`{"requestId":%q,"response":{"url":"https://example.test/final","status":200}}`, ids[0]+ids[1]))
	}
	if _, found, err := r.DocumentResponse("root-frame", "navigation"); err != nil || found {
		t.Fatalf("another frame/navigation supplied status: found=%v err=%v", found, err)
	}
	recordResponseEvent(t, c, "Network.requestWillBeSent", `{"requestId":"root","frameId":"root-frame","loaderId":"navigation","type":"Document","request":{"url":"https://example.test/final?secret=private","method":"GET"},"redirectResponse":{"status":302}}`)
	recordResponseEvent(t, c, "Network.responseReceived", `{"requestId":"root","frameId":"root-frame","loaderId":"navigation","type":"Document","response":{"url":"https://example.test/final?secret=private","status":404}}`)
	entry, found, err := r.DocumentResponse("root-frame", "navigation")
	if err != nil || !found || entry.Status != 404 || entry.OK || entry.URL != "https://example.test/final" {
		t.Fatalf("wrong exact response: %+v found=%v err=%v", entry, found, err)
	}
	entry.Status = 200
	entry, found, err = r.DocumentResponse("root-frame", "navigation")
	if err != nil || !found || entry.Status != 404 {
		t.Fatal("caller mutated recorder evidence")
	}
	recordResponseEvent(t, c, "Network.loadingFailed", `{"requestId":"root","errorText":"net::ERR_ABORTED"}`)
	if _, found, err := r.DocumentResponse("root-frame", "navigation"); err == nil || found {
		t.Fatal("failed document retained successful response proof")
	}
}

func TestDocumentResponseRejectsUnavailableOrConflictingEvidence(t *testing.T) {
	for _, r := range []*BrowserTraceRecorder{nil, {}, {active: true}} {
		if _, found, err := r.DocumentResponse("frame", "loader"); err == nil || found {
			t.Fatal("inactive recorder manufactured proof")
		}
	}
	for _, field := range []string{"frameId", "loaderId", "type"} {
		t.Run(field, func(t *testing.T) {
			c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
			r := &BrowserTraceRecorder{active: true, collector: c}
			recordResponseEvent(t, c, "Network.requestWillBeSent", `{"requestId":"root","frameId":"frame","loaderId":"loader","type":"Document","request":{"url":"https://example.test/","method":"GET"}}`)
			recordResponseEvent(t, c, "Network.responseReceived", fmt.Sprintf(`{"requestId":"root",%q:"conflicting","response":{"status":200}}`, field))
			if _, found, err := r.DocumentResponse("frame", "loader"); err == nil || found {
				t.Fatal("conflicting navigation identity supplied proof")
			}
		})
	}
	c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	r := &BrowserTraceRecorder{active: true, collector: c}
	for _, ids := range [][2]string{{"", "loader"}, {"frame", ""}} {
		if _, found, err := r.DocumentResponse(ids[0], ids[1]); err == nil || found {
			t.Fatal("missing navigation identity admitted")
		}
	}
	lost := errors.New("subscription lost events")
	c.setErr(lost)
	if _, found, err := r.DocumentResponse("frame", "loader"); !errors.Is(err, lost) || found {
		t.Fatal("lost protocol evidence ignored")
	}
}

func TestDocumentResponseCannotRelabelRedirectAtEvidenceLimit(t *testing.T) {
	c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	r := &BrowserTraceRecorder{active: true, collector: c}
	for index := 0; index < traceDebugEntryLimit; index++ {
		recordResponseEvent(t, c, "Network.requestWillBeSent", fmt.Sprintf(`{"requestId":"%d","frameId":"frame","loaderId":"loader","type":"Document","request":{"url":"https://example.test/old","method":"GET"}}`, index))
	}
	recordResponseEvent(t, c, "Network.requestWillBeSent", `{"requestId":"1023","frameId":"frame","loaderId":"loader","type":"Document","request":{"url":"https://example.test/final","method":"GET"}}`)
	recordResponseEvent(t, c, "Network.responseReceived", `{"requestId":"1023","response":{"status":404}}`)
	if _, found, err := r.DocumentResponse("frame", "loader"); err != nil || found {
		t.Fatalf("pending older request became a response: found=%v err=%v", found, err)
	}
	if c.network[traceDebugEntryLimit-1].Status != 0 || !c.truncated {
		t.Fatal("omitted redirect relabeled retained request")
	}
	if _, found, err := r.DocumentResponse("missing", "loader"); err == nil || found {
		t.Fatal("omitted response not reported as unavailable")
	}
}
