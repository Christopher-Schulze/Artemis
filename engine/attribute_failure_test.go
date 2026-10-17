package engine

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/js"
)

func TestActualHTTPAttributeFailureRejectsScriptAdmissionAndPreservesStaticFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/valid" {
			writeTestBody(t, w, `<title>before</title><button id="actual" onclick="document.title='actual attribute'">actual</button><script>document.getElementById('actual').click()</script>`)
			return
		}
		writeTestBody(t, w, `<title>actual static</title><button onclick="if (">actual</button>`)
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
	if page != nil {
		closeTestResource(t, "unexpected partial page", page.Close)
	}
	var listener *js.EventListenerError
	if page != nil || !errors.As(err, &listener) || !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("invalid handler admitted script-capable Page: page=%t error=%v", page != nil, err)
	}
	page, err = owner.Fetch(t.Context(), server.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("static fetch unnecessarily required executable handlers: %v", err)
	}
	if page.Title() != "actual static" {
		t.Error("static content was changed")
	}
	closeTestResource(t, "static page", page.Close)
	page, err = owner.Fetch(t.Context(), server.URL+"/valid", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("same Engine did not recover for valid attributes: %v", err)
	}
	defer closeTestResource(t, "valid page", page.Close)
	if page.Title() != "actual attribute" {
		t.Fatalf("valid handler did not execute: %q", page.Title())
	}
}
