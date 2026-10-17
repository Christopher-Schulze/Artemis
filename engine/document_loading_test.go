package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentLoadingThroughActualHTTPAndExternalScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/external.js" {
			writeTestBody(t, w, `trace.push('external:' + document.readyState);`)
			return
		}
		writeTestBody(t, w, `<html><head><title>pending</title></head><body><script>
		globalThis.trace = ['inline:' + document.readyState];
		document.addEventListener('readystatechange', function(e) {
		  trace.push('ready:' + document.readyState + ':' + (e.target === document) + ':' + (this === document));
		});
		document.addEventListener('DOMContentLoaded', function(e) {
		  trace.push('dom:' + document.readyState + ':' + (e.target === document)); document.title = 'Loaded';
		});
		window.addEventListener('DOMContentLoaded', e => trace.push('window-dom:' + (e.target === document)));
		window.onload = function(e) { trace.push('property-load:' + (e.target === document) + ':' + (this === window)); };
		window.addEventListener('load', function(e) { trace.push('load:' + document.readyState + ':' + (e.target === document) + ':' + (e.currentTarget === window)); });
		document.addEventListener('load', () => trace.push('wrong-document-load'));
		document.documentElement.addEventListener('DOMContentLoaded', () => trace.push('wrong-html-dom'));
		</script><script src='/external.js'></script></body></html>`)
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	value, err := page.Eval(t.Context(), "trace.join('|')")
	want := "inline:loading|external:loading|ready:interactive:true:true|dom:interactive:true|window-dom:true|ready:complete:true:true|property-load:true:true|load:complete:true:true"
	if err != nil || value.String() != want || page.Title() != "Loaded" {
		t.Fatalf("actual loading lifecycle mismatch: got=%q title=%q err=%v", value.String(), page.Title(), err)
	}
}

func TestDocumentLoadingListenerFailureRejectsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<script>document.addEventListener('DOMContentLoaded', () => { throw Error('actual-loading-failure'); });</script>`)
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
	if page != nil {
		closeTestResource(t, "unexpected page", page.Close)
	}
	if err == nil || !strings.Contains(err.Error(), "actual-loading-failure") || page != nil {
		t.Fatalf("loading callback failure returned a page: page=%t err=%v", page != nil, err)
	}
}

func TestScriptAndObserverFailuresRejectPartialPages(t *testing.T) {
	for _, script := range []string{
		`document.title = 'partial'; throw Error('actual-script-failure')`,
		`new MutationObserver(() => { document.title = 'partial'; throw Error('actual-observer-failure') }).observe(document.body, {attributes:true}); document.body.setAttribute('data-trigger','changed')`,
	} {
		t.Run(script, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeTestBody(t, w, "<html><head><title>before</title></head><body><script>"+script+"</script><script>document.title = 'must-not-run'</script></body></html>")
			}))
			defer server.Close()
			owner, err := New(testConfig(server))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "engine", owner.Close)
			page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
			if page != nil {
				closeTestResource(t, "unexpected page", page.Close)
			}
			if page != nil || err == nil || !strings.Contains(err.Error(), "actual-") {
				t.Fatalf("script failure acknowledged a partial page: page=%t err=%v", page != nil, err)
			}
		})
	}
}
