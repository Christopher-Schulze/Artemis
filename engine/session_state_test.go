package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestSessionIdentityReachesStylesFramesAndJavaScriptFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, err := r.Cookie("owner")
		value := "missing"
		if err == nil {
			value = owner.Value
		}
		switch r.URL.Path {
		case "/seed":
			http.SetCookie(w, &http.Cookie{Name: "owner", Value: "alice", Path: "/"})
			writeTestBody(t, w, "<html></html>")
		case "/page":
			writeTestBody(t, w, `<html><head><link rel="stylesheet" href="/style"></head><body><p id="styled">test</p><iframe id="frame" src="/frame"></iframe></body></html>`)
		case "/style":
			writeTestBody(t, w, fmt.Sprintf("#styled { color: %s; }", map[string]string{"alice": "red", "missing": "blue"}[value]))
		case "/frame":
			writeTestBody(t, w, `<html><body><p id="cookie"></p><script>document.getElementById('cookie').textContent=document.cookie; sessionStorage.setItem('frame','shared');</script></body></html>`)
		case "/fetch":
			writeTestBody(t, w, value)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	engine := mustNewTest(t, server)
	defer closeTestResource(t, "engine", engine.Close)
	seed, err := engine.Fetch(network.WithSessionID(context.Background(), "alice"), server.URL+"/seed", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, "seed", seed.Close)
	cases := []struct{ owner, color, cookie string }{{"alice", "rgb(255, 0, 0)", "owner=alice"}, {"bob", "rgb(0, 0, 255)", ""}}
	for _, tc := range cases {
		t.Run(tc.owner, func(t *testing.T) {
			p, err := engine.Fetch(network.WithSessionID(context.Background(), tc.owner), server.URL+"/page", FetchOpts{})
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "page", p.Close)
			checks := []struct{ expression, want string }{
				{"getComputedStyle(document.getElementById('styled')).color", tc.color},
				{"document.getElementById('frame').contentDocument.getElementById('cookie').textContent", tc.cookie},
				{"sessionStorage.getItem('frame')", "shared"},
			}
			for _, check := range checks {
				value, err := p.Eval(context.Background(), check.expression)
				if err != nil {
					t.Fatal(err)
				}
				if value.String() != check.want {
					t.Fatalf("%s=%q want=%q", check.expression, value.String(), check.want)
				}
			}
			if _, err := p.Eval(context.Background(), fmt.Sprintf("fetch(%q).then(r=>r.text()).then(v=>globalThis.fetchedOwner=v)", server.URL+"/fetch")); err != nil {
				t.Fatal(err)
			}
			value, err := p.Eval(context.Background(), "fetchedOwner")
			if err != nil {
				t.Fatal(err)
			}
			want := "missing"
			if tc.owner == "alice" {
				want = "alice"
			}
			if value.String() != want {
				t.Fatalf("JavaScript request owner=%q want=%q", value.String(), want)
			}
		})
	}
}

func TestCrossOriginFrameAndRedirectCannotExposeDocument(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, "<html><body><p>other-origin</p></body></html>")
	}))
	defer private.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, private.URL, http.StatusFound)
			return
		}
		writeTestBody(t, w, fmt.Sprintf(`<html><iframe id="direct" src="%s"></iframe><iframe id="redirect" src="/redirect"></iframe></html>`, private.URL))
	}))
	defer server.Close()
	engine := mustNewTest(t, server, private)
	defer closeTestResource(t, "engine", engine.Close)
	page := mustFetch(t, engine, server.URL, FetchOpts{})
	defer closeTestResource(t, "page", page.Close)
	for _, id := range []string{"direct", "redirect"} {
		t.Run(id, func(t *testing.T) {
			value, err := page.Eval(context.Background(), fmt.Sprintf("document.getElementById(%q).contentDocument===null", id))
			if err != nil {
				t.Fatal(err)
			}
			if !value.Bool() {
				t.Fatal("cross-origin document exposed")
			}
		})
	}
}
