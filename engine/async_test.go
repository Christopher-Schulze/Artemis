package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncFetchParallel(t *testing.T) {
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		for {
			old := maxInflight.Load()
			if cur <= old || maxInflight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		inflight.Add(-1)
		writeTestBody(t, w, fmt.Sprintf("ok %s", r.URL.Path))
	}))
	defer api.Close()

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, fmt.Sprintf(`<!doctype html><html><body><script>
			globalThis.results = [];
			Promise.all([
				fetch(%q + '/a').then(r => r.text()),
				fetch(%q + '/b').then(r => r.text()),
				fetch(%q + '/c').then(r => r.text()),
			]).then(rs => { globalThis.results = rs; });
		</script></body></html>`, api.URL, api.URL, api.URL))
	}))
	defer page.Close()

	eng, err := New(testConfig(api, page))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	start := time.Now()
	p, err := eng.Fetch(context.Background(), page.URL, FetchOpts{
		RunScripts: true,
		AsyncFetch: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", p.Close)
	if waitErr := p.WaitIdle(context.Background()); waitErr != nil {
		t.Fatalf("WaitIdle: %v", waitErr)
	}
	elapsed := time.Since(start)

	v, err := p.Eval(context.Background(), `globalThis.results.length`)
	if err != nil {
		t.Fatalf("Eval result length: %v", err)
	}
	if v.Int64() != 3 {
		t.Errorf("results.length = %d, want 3", v.Int64())
	}
	v, err = p.Eval(context.Background(), `globalThis.results.join(',')`)
	if err != nil {
		t.Fatalf("Eval result values: %v", err)
	}
	if !strings.Contains(v.String(), "ok /a") || !strings.Contains(v.String(), "ok /b") || !strings.Contains(v.String(), "ok /c") {
		t.Errorf("results = %q", v.String())
	}
	if maxInflight.Load() < 2 {
		t.Errorf("maxInflight = %d, want >= 2 (parallel)", maxInflight.Load())
	}
	// 3 sequential 50ms fetches would be ~150ms; parallel should be ~50-100ms.
	if elapsed > 130*time.Millisecond {
		t.Errorf("elapsed = %v, want < 130ms (parallel fetches)", elapsed)
	}
}

func TestAsyncFetchSequentialAwait(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, "hi from "+r.URL.Path)
	}))
	defer api.Close()

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, fmt.Sprintf(`<!doctype html><html><body><script>
			globalThis.captured = '';
			(async () => {
				const a = await (await fetch(%q + '/a')).text();
				const b = await (await fetch(%q + '/b')).text();
				globalThis.captured = a + ' | ' + b;
			})();
		</script></body></html>`, api.URL, api.URL))
	}))
	defer page.Close()

	eng := mustNewTest(t, api, page)
	defer closeTestResource(t, "engine close", eng.Close)
	p, err := eng.Fetch(context.Background(), page.URL, FetchOpts{
		RunScripts: true,
		AsyncFetch: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", p.Close)
	if waitErr := p.WaitIdle(context.Background()); waitErr != nil {
		t.Fatalf("WaitIdle: %v", waitErr)
	}
	v, err := p.Eval(context.Background(), `globalThis.captured`)
	if err != nil {
		t.Fatalf("Eval captured: %v", err)
	}
	if v.String() != "hi from /a | hi from /b" {
		t.Errorf("captured = %q", v.String())
	}
}

func TestAsyncWaitIdleCancel(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		writeTestBody(t, w, "slow")
	}))
	defer api.Close()

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, fmt.Sprintf(`<html><body><script>fetch(%q).then(()=>{});</script></body></html>`, api.URL))
	}))
	defer page.Close()

	eng := mustNewTest(t, api, page)
	defer closeTestResource(t, "engine close", eng.Close)
	p, err := eng.Fetch(context.Background(), page.URL, FetchOpts{
		RunScripts: true,
		AsyncFetch: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", p.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = p.WaitIdle(ctx)
	if err == nil {
		t.Fatal("expected ctx.Err on cancel")
	}
}
