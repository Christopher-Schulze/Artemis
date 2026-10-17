package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/platform"
)

type scopeMemoryOwner struct {
	mu         sync.Mutex
	held       int64
	releases   int
	deny       error
	releaseErr error
}

func (o *scopeMemoryOwner) ReserveMemory(ctx context.Context, size int64) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.deny != nil {
		return nil, o.deny
	}
	o.mu.Lock()
	o.held += size
	o.mu.Unlock()
	return func() error {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.held -= size
		o.releases++
		return o.releaseErr
	}, nil
}

func TestResponseScopeActualMappingAndStableValueAPI(t *testing.T) {
	payload := bytes.Repeat([]byte("actual-response-"), 100000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if _, err := w.Write(payload); err != nil {
			t.Errorf("write payload: %v", err)
		}
	}))
	defer server.Close()
	owner := &scopeMemoryOwner{}
	lifecycle := &testRequestLifecycle{}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner, RequestLifecycle: lifecycle})
	var borrowed *Response
	err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error {
		borrowed = response
		if !bytes.Equal(response.Body, payload) {
			return errors.New("actual body changed")
		}
		if client.mmap.State == platform.StateAvailable && (response.InputMemory.State != platform.StateAvailable || response.InputMemory.Value != uint64(len(payload)) || owner.held < int64(len(payload)) || owner.held >= int64(len(payload)+os.Getpagesize()) || owner.held%int64(os.Getpagesize()) != 0) {
			return fmt.Errorf("mapping not admitted through consumer lifetime: %+v held=%d", response.InputMemory, owner.held)
		}
		if lifecycle.finished != 1 {
			return errors.New("network concurrency retained into nested consumer work")
		}
		return nil
	})
	if err != nil || borrowed == nil || borrowed.Body != nil || owner.held != 0 {
		t.Fatalf("scope cleanup failed: %v held=%d", err, owner.held)
	}
	if client.mmap.State == platform.StateAvailable && owner.releases != 1 {
		t.Fatalf("release count=%d", owner.releases)
	}
	response, err := client.Do(t.Context(), Request{URL: server.URL})
	if err != nil || !bytes.Equal(response.Body, payload) || response.InputMemory.State == platform.StateAvailable {
		t.Fatalf("value API lost its owned Go bytes: %v", err)
	}
}

func TestResponseScopeRejectsAdmissionAndJoinsConsumerCleanupErrors(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), (1<<20)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		if _, err := w.Write(payload); err != nil {
			t.Logf("peer closed rejected response: %v", err)
		}
	}))
	defer server.Close()
	denied, consumerErr, cleanupErr := errors.New("capacity denied"), errors.New("consumer failed"), errors.New("settlement failed")
	for _, deny := range []bool{true, false} {
		owner := &scopeMemoryOwner{releaseErr: cleanupErr}
		if deny {
			owner.deny = denied
		}
		client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
		if client.mmap.State != platform.StateAvailable {
			continue
		}
		calls := 0
		err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error { calls++; return consumerErr })
		if deny {
			if !errors.Is(err, denied) || calls != 0 || owner.releases != 0 {
				t.Fatalf("denial reached consumer: %v calls=%d releases=%d", err, calls, owner.releases)
			}
		} else if !errors.Is(err, consumerErr) || !errors.Is(err, cleanupErr) || calls != 1 || owner.releases != 1 || owner.held != 0 {
			t.Fatalf("joined cleanup lost ownership/cause: %v calls=%d owner=%+v", err, calls, owner)
		}
	}
}

func TestResponseScopeReadFailureAndCancellationReleaseMapping(t *testing.T) {
	owner := &scopeMemoryOwner{}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
	if client.mmap.State != platform.StateAvailable {
		return
	}
	_, release, _, err := client.readScopedBody(t.Context(), bytes.NewReader([]byte("short")), (1<<20)+1, 2<<20, true)
	if err == nil || release == nil || owner.held != int64((1<<20)+os.Getpagesize()) {
		t.Fatalf("truncated read lost owned cleanup: %v", err)
	}
	if err := release(); err != nil || owner.held != 0 {
		t.Fatalf("release failed: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := client.readScopedBody(ctx, bytes.NewReader(nil), (1<<20)+1, 2<<20, true); !errors.Is(err, context.Canceled) || owner.held != 0 {
		t.Fatalf("canceled allocation admitted: %v", err)
	}
}
