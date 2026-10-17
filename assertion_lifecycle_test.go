package artemis

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

// The gate holds an actual policy decision after cancellation so teardown must
// join the admitted operation before disposing its real V8 page.
type assertionPolicyGate struct {
	delegate bridge.PolicyEngine
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (g *assertionPolicyGate) Evaluate(ctx context.Context, request bridge.PolicyRequest) (bridge.PolicyResponse, error) {
	if request.Action == "query" {
		close(g.entered)
		<-ctx.Done()
		close(g.canceled)
		<-g.release
	}
	return g.delegate.Evaluate(ctx, request)
}

func TestSessionCloseCancelsAndJoinsAdmittedAssertion(t *testing.T) {
	policy := fixtureActionPolicy(t)
	gate := &assertionPolicyGate{delegate: policy, entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	f := newAssertionFixture(t, AgentConfig{PolicyEngine: gate})
	ctx, cancel := context.WithCancel(t.Context())
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate.release) }) }
	assertDone := make(chan struct{})
	closeDone := make(chan struct{})
	var taskErr *TaskError
	var closeErr error
	go func() {
		defer close(assertDone)
		_, taskErr = f.session.Assert(ctx, f.pageID, "title_contains", "", "Measured", "", 0, nil)
	}()
	defer func() {
		cancel()
		release()
		select {
		case <-assertDone:
		case <-time.After(2 * time.Second):
			t.Error("assertion worker did not join after rescue cancellation")
		}
	}()
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("assertion did not reach the real query policy")
	}
	go func() {
		defer close(closeDone)
		closeErr = f.agent.CloseSession(f.session.SessionID())
	}()
	defer func() {
		cancel()
		release()
		select {
		case <-closeDone:
		case <-time.After(2 * time.Second):
			t.Error("session close did not join after rescue cancellation")
		}
	}()
	select {
	case <-gate.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("session close did not cancel the admitted assertion")
	}
	select {
	case <-closeDone:
		t.Fatal("session close returned while its admitted assertion remained held")
	case <-time.After(100 * time.Millisecond):
	}
	if f.session.Page(f.pageID) == nil {
		t.Fatal("session close disposed the page before joining its admitted assertion")
	}
	release()
	select {
	case <-assertDone:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled assertion did not return")
	}
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("session close did not join the completed assertion")
	}
	if taskErr == nil || taskErr.Code != TaskErrorCancelled || closeErr != nil || f.session.IsActive() || f.session.Page(f.pageID) != nil {
		t.Fatalf("joined lifecycle: assertion=%v close=%v active=%v page=%v", taskErr, closeErr, f.session.IsActive(), f.session.Page(f.pageID))
	}
}
