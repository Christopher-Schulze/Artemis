package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestSecurityGateSessionRequestResponseAndConcurrencyExhaustion(t *testing.T) {
	t.Run("request count", func(t *testing.T) {
		limits := securityGateSessionBudget()
		limits.MaxRequests = 1
		controller := newSessionBudgetController(limits, "request-gate", nil)
		defer closeTestResource(t, "request gate", controller.close)
		_, finish, err := controller.BeginRequest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := finish(0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := controller.BeginRequest(context.Background()); !IsBudgetExceeded(err) {
			t.Fatalf("request exhaustion error=%v", err)
		}
		usage := controller.sessionUsage("request-gate")
		if usage.Requests != 1 || usage.Concurrent != 0 || !usage.Cancelled {
			t.Fatalf("request exhaustion usage=%+v", usage)
		}
		if _, finishOther, err := controller.BeginRequest(network.WithSessionID(context.Background(), "other")); err != nil {
			t.Fatalf("another session was cancelled by this session's breach: %v", err)
		} else if err := finishOther(0); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("response bytes", func(t *testing.T) {
		limits := securityGateSessionBudget()
		limits.MaxResponseBytes = 4
		controller := newSessionBudgetController(limits, "response-gate", nil)
		defer closeTestResource(t, "response gate", controller.close)
		_, finish, err := controller.BeginRequest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := finish(5); !IsBudgetExceeded(err) {
			t.Fatalf("response exhaustion error=%v", err)
		}
		usage := controller.sessionUsage("response-gate")
		if usage.ResponseBytes != 0 || usage.Concurrent != 0 || !usage.Cancelled {
			t.Fatalf("response exhaustion usage=%+v", usage)
		}
	})

	t.Run("concurrency", func(t *testing.T) {
		limits := securityGateSessionBudget()
		limits.MaxConcurrency = 1
		controller := newSessionBudgetController(limits, "concurrency-gate", nil)
		defer closeTestResource(t, "concurrency gate", controller.close)
		requestCtx, finish, err := controller.BeginRequest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := controller.BeginRequest(context.Background()); !IsBudgetExceeded(err) {
			t.Fatalf("concurrency exhaustion error=%v", err)
		}
		// Concurrency is admission control: the in-flight request continues.
		select {
		case <-requestCtx.Done():
			t.Fatalf("in-flight request was cancelled by a refused admission: %v", context.Cause(requestCtx))
		case <-time.After(100 * time.Millisecond):
		}
		if err := finish(0); err != nil {
			t.Fatalf("finish after refused concurrent admission error=%v", err)
		}
		if usage := controller.usage(); usage.Concurrent != 0 || usage.Cancelled {
			t.Fatalf("concurrency lease leaked or engine cancelled: %+v", usage)
		}
		if _, again, err := controller.BeginRequest(context.Background()); err != nil {
			t.Fatalf("freed concurrency slot not reusable: %v", err)
		} else if err := again(0); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSecurityGateConcurrentParentCancellationReleasesEveryRequest(t *testing.T) {
	limits := securityGateSessionBudget()
	const workers = 32
	limits.MaxConcurrency = workers
	limits.MaxRequests = workers
	controller := newSessionBudgetController(limits, "cancel-gate", nil)
	defer closeTestResource(t, "cancellation gate", controller.close)
	parent, cancel := context.WithCancel(context.Background())
	contexts := make([]context.Context, workers)
	finishes := make([]func(int64) error, workers)
	for worker := 0; worker < workers; worker++ {
		requestCtx, finish, err := controller.BeginRequest(parent)
		if err != nil {
			t.Fatal(err)
		}
		contexts[worker] = requestCtx
		finishes[worker] = finish
	}
	cancel()
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case <-contexts[worker].Done():
				if !errors.Is(context.Cause(contexts[worker]), context.Canceled) {
					errorsFound <- context.Cause(contexts[worker])
					return
				}
			case <-time.After(time.Second):
				errorsFound <- errors.New("request survived parent cancellation")
				return
			}
			if err := finishes[worker](0); err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent cancellation: %v", err)
	}
	usage := controller.usage()
	if usage.Requests != workers || usage.Concurrent != 0 || usage.Cancelled {
		t.Fatalf("cancellation accounting=%+v", usage)
	}
}

func securityGateSessionBudget() SessionBudget {
	return SessionBudget{
		MaxTabs: 8, MaxRequests: 128, MaxResponseBytes: 1 << 20,
		MaxDiskBytes: 1 << 20, MaxConcurrency: 64, Timeout: time.Minute,
	}
}
