package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPageAbortSignalRetiresActualHTTPAndPreservesReason(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			close(entered)
			<-request.Context().Done()
			close(cancelled)
			return
		}
		writeTestBody(t, w, `<html><head><title>pending</title></head><body><script>
		globalThis.controller=new AbortController(), reason={actual:'HTTP abort'}, observed='';
		fetch('/slow',{signal:controller.signal}).then(()=>{observed='invalid success'},error=>{
		  observed=String(error===reason); document.title='aborted';
		});
		</script></body></html>`)
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	page, err := owner.Fetch(ctx, server.URL, FetchOpts{RunScripts: true, AsyncFetch: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual HTTP request did not enter server")
	}
	if _, err := page.EvalString(ctx, "controller.abort(reason); 'aborted'"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("AbortSignal did not cancel actual HTTP request context")
	}
	if err := page.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := page.EvalString(ctx, "observed")
	if err != nil || got != "true" || page.Title() != "aborted" {
		t.Fatalf("actual HTTP abort lost reason or DOM effect: result=%q title=%q err=%v", got, page.Title(), err)
	}
	got, err = page.EvalString(ctx, "String(6*7)")
	if err != nil || got != "42" {
		t.Fatalf("request abort poisoned later page execution: %q %v", got, err)
	}
}
