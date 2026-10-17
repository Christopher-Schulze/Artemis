package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestMappedResponseEngineKeepsOwnedPageAndNestedScript(t *testing.T) {
	payload := "<html><title>Mapped</title><body>" + strings.Repeat("data ", 250000) + "<script src='/script.js'></script></body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := payload
		if r.URL.Path == "/script.js" {
			body = "globalThis.mappedNestedScript = 'Nested script ran';"
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		writeTestBody(t, w, body)
	}))
	defer server.Close()
	cfg := testConfig(server)
	cfg.SessionBudget.MaxConcurrency = 1
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	value, err := page.Eval(t.Context(), "globalThis.mappedNestedScript")
	if err != nil || value.String() != "Nested script ran" || page.Title() != "Mapped" || string(page.RawBody()) != payload || eng.SessionUsage().MemoryBytes != 0 || eng.SessionUsage().Concurrent != 0 {
		t.Fatalf("mapped response leaked or invalidated page: title=%q usage=%+v", page.Title(), eng.SessionUsage())
	}
}

func TestScreenshotInputUsesSharedQuotaAndPrivateBootAdmission(t *testing.T) {
	owner, err := New(Config{SessionBudget: SessionBudget{MaxMemoryBytes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	}()
	pipeline := bridge.NewJPEGPipeline()
	raw := bytes.Repeat([]byte{31, 73, 97}, 400000)
	encoded := base64.StdEncoding.EncodeToString(raw)
	pipeline.Caps.HasMmap = false
	called := false
	err = pipeline.WithScreenshotInput(t.Context(), encoded, owner.InputMemoryAdmission(), func([]byte, platform.ProbeResult) error { called = true; return nil })
	if !IsBudgetExceeded(err) || called || owner.SessionUsage().MemoryBytes != 0 {
		t.Fatalf("public capability mutation or Go fallback bypassed real quota: %v called=%v", err, called)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called = false
	if err := pipeline.WithScreenshotInput(ctx, encoded, nil, func([]byte, platform.ProbeResult) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled input admitted: %v called=%v", err, called)
	}
}

func TestMappedMemoryCapacityIsHeldUntilVerifiedRelease(t *testing.T) {
	limits := SessionBudget{MaxMemoryBytes: 8}
	limits.applyDefaults(1024)
	controller := newSessionBudgetController(limits, "owner", nil)
	defer closeTestResource(t, "budget", controller.close)
	release, err := controller.ReserveMemory(t.Context(), 6)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := controller.ReserveMemory(ctx, 1); !errors.Is(err, context.Canceled) || controller.usage().MemoryBytes != 6 {
		t.Fatalf("cancellation released live capacity: %v", err)
	}
	if _, err := controller.ReserveMemory(t.Context(), 3); !IsBudgetExceeded(err) || controller.usage().MemoryBytes != 6 {
		t.Fatalf("aggregate cap bypassed: %v", err)
	}
	if err := release(); err != nil || controller.usage().MemoryBytes != 0 {
		t.Fatalf("actual release failed: %v", err)
	}
	if err := release(); err != nil || controller.usage().MemoryBytes != 0 {
		t.Fatalf("duplicate release changed accounting: %v", err)
	}
}

func TestSharedMemoryAdmissionDoesNotReplaceSessionAndRollsBackDenial(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		local, shared, request int64
	}{
		{name: "local denial", local: 1, shared: 8, request: 2},
		{name: "shared denial", local: 8, shared: 1, request: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			localLimits := SessionBudget{MaxMemoryBytes: tc.local}
			localLimits.applyDefaults(1024)
			sharedLimits := SessionBudget{MaxMemoryBytes: tc.shared}
			sharedLimits.applyDefaults(1024)
			local := newSessionBudgetController(localLimits, "local", nil)
			shared := newSessionBudgetController(sharedLimits, "shared", nil)
			local.sharedMemory = shared
			defer closeTestResource(t, "local", local.close)
			defer closeTestResource(t, "shared", shared.close)
			if _, err := local.ReserveMemory(t.Context(), tc.request); !IsBudgetExceeded(err) || local.usage().MemoryBytes != 0 || shared.usage().MemoryBytes != 0 {
				t.Fatalf("denial bypassed or leaked budget: %v", err)
			}
		})
	}
}

func TestEngineRejectsTypedNilMemoryOwner(t *testing.T) {
	var owner *sessionBudgetController
	if _, err := New(Config{MemoryAdmission: owner}); err == nil {
		t.Fatal("typed-nil memory owner admitted")
	}
}

func TestTransientMemoryOverflowAndLightweightOwner(t *testing.T) {
	owner, closeOwner, err := NewMemoryAdmission(SessionBudget{MaxMemoryBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "memory owner", closeOwner)
	release, err := owner.ReserveMemory(t.Context(), 6)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "memory claim", release)
	_, err = owner.ReserveMemory(t.Context(), math.MaxInt64)
	var breach *BudgetError
	if !errors.As(err, &breach) || breach.Observed != math.MaxInt64 || breach.Resource != BudgetMemoryBytes {
		t.Fatalf("memory overflow lost truthful rejection: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// Transient memory is admission control: the rejected claim does not
	// poison later claims that fit.
	reclaimed, err := owner.ReserveMemory(t.Context(), 8)
	if err != nil {
		t.Fatalf("memory owner stayed poisoned after a refused claim: %v", err)
	}
	if _, err := owner.ReserveMemory(t.Context(), 1); !IsBudgetExceeded(err) {
		t.Fatal("claim above the remaining capacity was admitted")
	}
	if err := reclaimed(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewMemoryAdmission(SessionBudget{MaxMemoryBytes: -1}); err == nil {
		t.Fatal("negative lightweight memory budget admitted")
	}
}
