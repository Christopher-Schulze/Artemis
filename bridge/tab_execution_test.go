package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func executionTestPage(owner *BrowserContext, id string) *Page {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Page{owner: owner, targetID: id, state: TargetStateAttached, lifetime: lifetime, cancelLifetime: cancel}
}

func TestPageExecutionSharesContextBoundAndCancelsOnTargetLoss(t *testing.T) {
	owner := &BrowserContext{pages: make(map[string]*Page)}
	if err := owner.SetMaxParallelTabs(1); err != nil {
		t.Fatal(err)
	}
	first, second := executionTestPage(owner, "first"), executionTestPage(owner, "second")
	defer first.cancelLifetime()
	defer second.cancelLifetime()
	lease, err := first.AcquireExecution(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if acquired, err := second.AcquireExecution(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		if acquired != nil {
			acquired.Release()
		}
		t.Fatalf("second target bypassed shared bound: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		queued, err := first.AcquireExecution(t.Context())
		if queued != nil {
			queued.Release()
		}
		done <- err
	}()
	first.setState(TargetStateCrashed, "renderer crash")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("lost target admitted queued work")
		}
	case <-time.After(time.Second):
		t.Fatal("target loss retained queued work")
	}
	lease.Release()
	var invoked bool
	if err := lease.Run(first, func(context.Context) { invoked = true }); err == nil || invoked {
		t.Fatalf("released lease accepted action: %v ran=%v", err, invoked)
	}
}

func TestPageLeaseValidatesIdentityAndJoinsActiveUse(t *testing.T) {
	owner := &BrowserContext{pages: make(map[string]*Page)}
	page := executionTestPage(owner, "owned")
	defer page.cancelLifetime()
	lease, err := page.AcquireExecution(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := lease.Run(&Page{}, func(context.Context) { t.Error("foreign page action executed") }); err == nil {
		t.Fatal("foreign lease identity accepted")
	}
	entered, finish := make(chan struct{}), make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		runDone <- lease.Run(page, func(context.Context) { close(entered); <-finish })
	}()
	<-entered
	released := make(chan struct{})
	var releases sync.WaitGroup
	releases.Add(1)
	go func() { defer releases.Done(); lease.Release(); close(released) }()
	select {
	case <-released:
		t.Fatal("release completed while lease action was running")
	case <-time.After(10 * time.Millisecond):
	}
	close(finish)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	releases.Wait()
	if replacement, err := page.AcquireExecution(t.Context()); err != nil {
		t.Fatal(err)
	} else {
		replacement.Release()
	}
}
