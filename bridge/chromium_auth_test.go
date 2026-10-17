package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAuthChallengeAdmissionIsBoundedAndRetainsNoPrivateRequestData(t *testing.T) {
	event := CDPEvent{SessionID: "owned", Method: "Fetch.authRequired", Params: json.RawMessage(`{"requestId":"actual-request","request":{"url":"https://private.invalid/?secret=value","headers":{"Authorization":"private"}},"authChallenge":{"realm":"private","origin":"private"}}`)}
	job, err := decodeAuthChallengeJob(event)
	if err != nil || !job.authRequired || job.sessionID != "owned" || job.payload.RequestID != "actual-request" {
		t.Fatalf("auth admission: %+v %v", job, err)
	}
	if job.payload.Request.URL != "" || job.payload.Request.Headers != nil || job.payload.Request.PostData != "" {
		t.Fatal("auth cancellation retained private request metadata")
	}
	for _, input := range []string{`{}`, `null`, `{"requestId":null}`, `{"requestId":3}`, `{"requestId":""}`, `{"requestId":"\n"}`, `{"requestId":"` + strings.Repeat("x", 257) + `"}`, strings.Repeat(" ", maxAuthChallengeEventBytes+1), string([]byte{0xff})} {
		event.Params = json.RawMessage(input)
		if got, err := decodeAuthChallengeJob(event); err == nil || got.authRequired {
			t.Fatalf("invalid auth event queued: %+v %v", got, err)
		}
	}
}

func TestAuthChallengeQueueFiltersForeignSessionsAndFailsClosed(t *testing.T) {
	page := &Page{sessionID: "owned", childSessions: map[string]string{"child": "iframe"}}
	jobs := make(chan fetchRequestJob, 1)
	event := CDPEvent{SessionID: "foreign", Method: "Fetch.authRequired", Params: json.RawMessage(`invalid private payload`)}
	if !page.handleFetchEvent(event, jobs) || len(jobs) != 0 {
		t.Fatal("foreign auth event affected this page")
	}
	event.SessionID, event.Params = "child", json.RawMessage(`{"requestId":"actual"}`)
	if !page.handleFetchEvent(event, jobs) || len(jobs) != 1 {
		t.Fatal("owned child auth event was not admitted")
	}
	if page.handleFetchEvent(event, jobs) {
		t.Fatal("full auth queue was silently accepted")
	}
	if job := <-jobs; !job.authRequired || job.sessionID != "child" {
		t.Fatalf("wrong policy work: %+v", job)
	}
	event.Params = json.RawMessage(`{}`)
	if page.handleFetchEvent(event, jobs) || len(jobs) != 0 {
		t.Fatal("malformed owned challenge was accepted")
	}
}

func TestAuthCancellationRejectsMissingCanceledRetiredAndForeignOwners(t *testing.T) {
	if err := (*Page)(nil).cancelAuthChallenge(nil, "owned", "request"); err == nil {
		t.Fatal("missing owner/context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (*Page)(nil).cancelAuthChallenge(ctx, "owned", "request"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	page := &Page{sessionID: "owned", state: TargetStateAttached, owner: &BrowserContext{browser: &ChromiumBrowser{}, closed: true}}
	if err := page.cancelAuthChallenge(context.Background(), "owned", "request"); err == nil {
		t.Fatal("closed context admitted auth work")
	}
	page.owner.closed = false
	if err := page.cancelAuthChallenge(context.Background(), "foreign", "request"); err == nil {
		t.Fatal("foreign session admitted auth work")
	}
	page.state = TargetStateClosed
	if err := page.cancelAuthChallenge(context.Background(), "owned", "request"); err == nil {
		t.Fatal("retired page admitted auth work")
	}
	data, err := json.Marshal(fetchContinueAuthParams{RequestID: "actual", AuthChallengeResponse: fetchAuthChallengeResponse{Response: "CancelAuth"}})
	if err != nil || string(data) != `{"requestId":"actual","authChallengeResponse":{"response":"CancelAuth"}}` {
		t.Fatalf("cancellation changed or exposed credentials: %s %v", data, err)
	}
}
