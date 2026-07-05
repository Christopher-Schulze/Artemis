package js

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

// TestPooledContextReusesV8Ctx verifies that with a non-zero pool size,
// the same v8.Context is reused across NewContext / Close cycles.
func TestPooledContextReusesV8Ctx(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()

	doc1, _ := parser.ParseHTML(strings.NewReader("<html><body><p id=p>1</p></body></html>"), "https://example.com/a")
	c1, err := rt.NewContext(doc1, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	v8a := c1.v8ctx
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html><body><p id=p>2</p></body></html>"), "https://example.com/b")
	c2, err := rt.NewContext(doc2, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext (pool): %v", err)
	}
	defer c2.Close()
	if c2.v8ctx != v8a {
		t.Errorf("expected pooled v8.Context to be reused, got fresh one")
	}
}

// TestPooledContextResetsGlobals verifies that user-set globals from a
// previous page do not leak into the next page through the pool.
func TestPooledContextResetsGlobals(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/")

	c1, err := rt.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	if _, err := c1.Eval(context.Background(), `globalThis.__leaktest = 'page1'`); err != nil {
		t.Fatalf("set leaktest: %v", err)
	}
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/")
	c2, err := rt.NewContext(doc2, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext (pool): %v", err)
	}
	defer c2.Close()
	v, err := c2.Eval(context.Background(), `typeof globalThis.__leaktest`)
	if err != nil {
		t.Fatalf("read leaktest: %v", err)
	}
	if v.String() != "undefined" {
		t.Errorf("user global leaked: got %q, want %q", v.String(), "undefined")
	}
}

// TestPooledContextLocationRebind verifies that location.* reflects the
// new page URL after pool reuse.
func TestPooledContextLocationRebind(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()

	doc1, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/first")
	c1, _ := rt.NewContext(doc1, ContextOpts{})
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/second")
	c2, err := rt.NewContext(doc2, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c2.Close()
	v, _ := c2.Eval(context.Background(), `location.href`)
	if v.String() != "https://example.com/second" {
		t.Errorf("location.href: got %q, want %q", v.String(), "https://example.com/second")
	}
	v, _ = c2.Eval(context.Background(), `location.pathname`)
	if v.String() != "/second" {
		t.Errorf("location.pathname: got %q, want %q", v.String(), "/second")
	}
}

// TestPooledContextStorageIsolation verifies that localStorage values
// set on a previous page do not leak into the next page.
func TestPooledContextStorageIsolation(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/")

	c1, _ := rt.NewContext(doc, ContextOpts{})
	if _, err := c1.Eval(context.Background(), `localStorage.setItem('k', 'v1')`); err != nil {
		t.Fatalf("setItem: %v", err)
	}
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://example.com/")
	c2, _ := rt.NewContext(doc2, ContextOpts{})
	defer c2.Close()
	v, _ := c2.Eval(context.Background(), `localStorage.getItem('k')`)
	if v.String() != "" && v.String() != "null" {
		t.Errorf("localStorage leaked: got %q", v.String())
	}
}

// TestPooledContextCustomElementsReset verifies that customElements
// definitions from a previous page do not block re-definition on the
// next page.
func TestPooledContextCustomElementsReset(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html><body><my-el></my-el></body></html>"), "https://example.com/")

	c1, _ := rt.NewContext(doc, ContextOpts{})
	if _, err := c1.Eval(context.Background(), `customElements.define('my-el', class {})`); err != nil {
		t.Fatalf("define page1: %v", err)
	}
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html><body><my-el></my-el></body></html>"), "https://example.com/")
	c2, _ := rt.NewContext(doc2, ContextOpts{})
	defer c2.Close()
	if _, err := c2.Eval(context.Background(), `customElements.define('my-el', class {})`); err != nil {
		t.Errorf("define page2 should not throw 'already defined': %v", err)
	}
}

// TestPooledContextFunctionalCoverage runs DOM/console exercises on a
// pool-reused Context to make sure bindings still work after reset.
func TestPooledContextFunctionalCoverage(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html><body><div id=d>hi</div></body></html>"), "https://example.com/")

	c1, _ := rt.NewContext(doc, ContextOpts{})
	c1.Close()

	doc2, _ := parser.ParseHTML(strings.NewReader("<html><body><div id=d>second</div></body></html>"), "https://example.com/")
	c2, _ := rt.NewContext(doc2, ContextOpts{})
	defer c2.Close()
	v, err := c2.Eval(context.Background(), `document.getElementById('d').textContent`)
	if err != nil {
		t.Fatalf("DOM eval after pool reuse: %v", err)
	}
	if v.String() != "second" {
		t.Errorf("DOM after reuse: got %q, want 'second'", v.String())
	}
}

// TestWarmPoolNoFirstPageColdCost verifies that NewRuntimeWithWarmPool
// pre-builds all N v8.Contexts so the first NewContext call gets the
// fast pool path. We assert that the same v8.Context returned on first
// use was pre-allocated (was in the pool before the first NewContext).
func TestWarmPoolNoFirstPageColdCost(t *testing.T) {
	rt, err := NewRuntimeWithWarmPool(2)
	if err != nil {
		t.Fatalf("NewRuntimeWithWarmPool: %v", err)
	}
	defer rt.Close()

	// Pool must contain 2 ready contexts immediately.
	if len(rt.ctxPool) != 2 {
		t.Errorf("pool size after warmup: got %d, want 2", len(rt.ctxPool))
	}
	doc, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://e.test/")
	c, err := rt.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()
	// After grabbing from pool, only 1 should remain.
	if len(rt.ctxPool) != 1 {
		t.Errorf("pool size after first NewContext: got %d, want 1", len(rt.ctxPool))
	}
}

// TestPooledContextSerialReuseStress exercises pool reuse across many
// serial NewContext / Close cycles (more than pool size) to catch
// reuse-cycle leaks or degradation.
func TestPooledContextSerialReuseStress(t *testing.T) {
	rt := NewRuntimeWithPool(4)
	defer rt.Close()
	for i := 0; i < 30; i++ {
		doc, _ := parser.ParseHTML(
			strings.NewReader("<html><body><p id=p>page-"+itos(i)+"</p></body></html>"),
			"https://e.test/",
		)
		c, err := rt.NewContext(doc, ContextOpts{})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		v, err := c.Eval(context.Background(), `document.getElementById('p').textContent`)
		if err != nil {
			t.Fatalf("iter %d eval: %v", i, err)
		}
		if v.String() != "page-"+itos(i) {
			t.Errorf("iter %d: got %q, want page-%s", i, v.String(), itos(i))
		}
		c.Close()
	}
}

// TestPooledContextConcurrent stress-tests pool reuse under concurrent
// NewContext / Eval / Close cycles. The Runtime.ctxMu lock is what
// makes this safe — without it, V8 GlobalHandles bookkeeping races
// across goroutines and crashes.
func TestPooledContextConcurrent(t *testing.T) {
	rt := NewRuntimeWithPool(8)
	defer rt.Close()
	runConcurrentNewContext(t, rt, 50)
}

// TestNonPooledContextConcurrent: same as above but without the pool,
// to verify ctxMu protects the install* path equally.
func TestNonPooledContextConcurrent(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	runConcurrentNewContext(t, rt, 50)
}

func runConcurrentNewContext(t *testing.T, rt *Runtime, n int) {
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			html := "<html><body><p id=p>page-" + itos(i) + "</p></body></html>"
			doc, err := parser.ParseHTML(strings.NewReader(html), "https://e.test/")
			if err != nil {
				errs <- "parse: " + err.Error()
				return
			}
			c, err := rt.NewContext(doc, ContextOpts{})
			if err != nil {
				errs <- "NewContext: " + err.Error()
				return
			}
			defer c.Close()
			v, err := c.Eval(context.Background(), `document.getElementById('p').textContent`)
			if err != nil {
				errs <- "eval: " + err.Error()
				return
			}
			want := "page-" + itos(i)
			if v.String() != want {
				errs <- "got " + v.String() + " want " + want
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// itos is a tiny int->string helper to avoid pulling in strconv.
func itos(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = '0' + byte(i%10)
		i /= 10
	}
	return string(b[pos:])
}
