package artemis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

func TestAgentSessionsSeparateCookiesAndStorageThroughRealHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/seed" {
			http.SetCookie(w, &http.Cookie{Name: "owner", Value: "alice", Path: "/"})
		}
		if _, err := fmt.Fprintf(w, "<!doctype html><title>Session</title><p>%s</p>", r.Header.Get("Cookie")); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent := mustAgent(t, AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	}()
	alice, err := agent.CreateSession("alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := agent.CreateSession("bob")
	if err != nil {
		t.Fatal(err)
	}
	open := func(session *Session, path string) *engine.Page {
		t.Helper()
		_, p, e := session.OpenPage(ctx, server.URL+path, true)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	seed := open(alice, "/seed")
	if _, err := seed.Eval(ctx, "localStorage.setItem('owner','alice'); document.cookie='js=alice; path=/'"); err != nil {
		t.Fatal(err)
	}
	bobPage := open(bob, "/read")
	if bobPage.Text() != "" {
		t.Fatalf("Bob used Alice's HTTP cookies: %q", bobPage.Text())
	}
	value, err := bobPage.Eval(ctx, "document.cookie+':'+localStorage.getItem('owner')")
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != ":null" {
		t.Fatalf("Bob used Alice's JS state: %q", value.String())
	}
	alicePage := open(alice, "/read")
	value, err = alicePage.Eval(ctx, "localStorage.getItem('owner')")
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "alice" {
		t.Fatal("Alice lost owned localStorage")
	}
	if err := agent.CloseSession(alice.SessionID()); err != nil {
		t.Fatal(err)
	}
	charlie, err := agent.CreateSession("charlie")
	if err != nil {
		t.Fatal(err)
	}
	if page := open(charlie, "/read"); page.Text() != "" {
		t.Fatalf("new owner reused closed authentication: %q", page.Text())
	}
	if _, _, e := alice.OpenPage(ctx, server.URL+"/read", false); e == nil {
		t.Fatal("closed session reopened")
	}
}

func TestSessionCloseCancelsRealRequestBeforeStateRelease(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(entered)
			<-r.Context().Done()
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "owner", Value: "private", Path: "/"})
		if _, err := fmt.Fprint(w, "<html></html>"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent := mustAgent(t, AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := agent.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	}()
	session, err := agent.CreateSession("owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.OpenPage(ctx, server.URL+"/seed", false); err != nil {
		t.Fatal(err)
	}
	operationDone := make(chan *TaskError, 1)
	go func() { _, _, err := session.OpenPage(ctx, server.URL+"/slow", false); operationDone <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request did not start")
	}
	if err := agent.CloseSession(session.SessionID()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operationDone:
		if err == nil {
			t.Fatal("cancelled session returned a successful page")
		}
	case <-ctx.Done():
		t.Fatal("request outlived close")
	}
	runtime, ok := agent.runtime.(*ownedEngineRuntime)
	if !ok {
		t.Fatal("production runtime missing")
	}
	jar, err := runtime.HTTPClient().CookieJarForSession(session.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(jar.Cookies(u)) != 0 {
		t.Fatal("closed owner retained authentication")
	}
}
