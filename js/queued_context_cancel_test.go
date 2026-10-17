package js

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type admissionObservedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *admissionObservedContext) Err() error {
	err := c.Context.Err()
	if err == nil {
		c.once.Do(func() { close(c.checked) })
	}
	return err
}

func TestQueuedEvalCannotPerformEffectsAfterOwnerCancellation(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	if _, err := c.Eval(t.Context(), "globalThis.queuedEffect = false"); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &admissionObservedContext{Context: parent, checked: make(chan struct{})}
	c.rt.ctxMu.Lock()
	var unlock sync.Once
	defer unlock.Do(c.rt.ctxMu.Unlock)
	done := make(chan error, 1)
	go func() { _, err := c.Eval(ctx, "queuedEffect = true"); done <- err }()
	select {
	case <-ctx.checked:
	case <-time.After(time.Second):
		t.Fatal("queued Eval never performed its real initial admission")
	}
	cancel()
	unlock.Do(c.rt.ctxMu.Unlock)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued Eval lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled queued Eval retained ownership")
	}
	value, err := c.Eval(t.Context(), "queuedEffect")
	if err != nil || value.Bool() {
		t.Fatalf("cancelled queued Eval performed a real JS effect: %v", err)
	}
}
