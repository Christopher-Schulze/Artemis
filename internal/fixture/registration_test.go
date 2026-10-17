package fixture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistrationRejectsInvalidAndAliasedRoutesWithoutPublication(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Scenario)
	}{
		{"missing ID", func(sc *Scenario) { sc.ID = "" }},
		{"root route", func(sc *Scenario) { sc.Path = "/" }},
		{"query", func(sc *Scenario) { sc.Path = "/route?x=1" }},
		{"fragment", func(sc *Scenario) { sc.Path = "/route#x" }},
		{"encoded literal alias", func(sc *Scenario) { sc.Path = "/%72oute" }},
		{"encoded separator alias", func(sc *Scenario) { sc.Path = "/route%2Fchild" }},
		{"double separator", func(sc *Scenario) { sc.Path = "/route//child" }},
		{"traversal", func(sc *Scenario) { sc.Path = "/route/../child" }},
		{"ServeMux wildcard", func(sc *Scenario) { sc.Path = "/{route}" }},
		{"bad kind", func(sc *Scenario) { sc.Kind = "invalid" }},
		{"bad status", func(sc *Scenario) { sc.Status = 600 }},
		{"bad expectation", func(sc *Scenario) { sc.Expect = Expect{Eval: "1"} }},
		{"typed nil handler", func(sc *Scenario) { var handler http.HandlerFunc; sc.Handler = handler }},
		{"bad content type", func(sc *Scenario) { sc.ContentType = "text/html\r\nX-Bad: 1" }},
		{"oversized metadata", func(sc *Scenario) { sc.Description = strings.Repeat("x", 16<<10+1) }},
		{"negative byte estimate", func(sc *Scenario) { sc.BodyBytes = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := NewServer()
			defer server.Close()
			sc := Scenario{ID: "route", Path: "/route", Kind: KindHTML, HTML: "original"}
			tc.change(&sc)
			if err := server.Register(sc); err == nil {
				t.Fatal("invalid fixture admitted")
			}
			if len(server.Scenarios()) != 0 || server.registeredBytes != 0 {
				t.Fatal("invalid fixture partially published")
			}
		})
	}
}

func TestRegistrationBatchCollisionIsAtomicAndDataIsDetached(t *testing.T) {
	server := NewServer()
	defer server.Close()
	sc := Scenario{ID: "original", Path: "/original", Kind: KindHTML, HTML: "original body", Expect: Expect{Contains: []string{"original"}, NotContains: []string{}}}
	if err := server.Register(sc); err != nil {
		t.Fatal(err)
	}
	sc.Expect.Contains[0] = "caller mutation"
	for _, batch := range [][]Scenario{
		{{ID: "new", Path: "/new", Kind: KindHTML}, {ID: "original", Path: "/different", Kind: KindHTML}},
		{{ID: "new", Path: "/new", Kind: KindHTML}, {ID: "different", Path: "/original", Kind: KindHTML}},
		{{ID: "new", Path: "/new", Kind: KindHTML}, {ID: "same", Path: "/new", Kind: KindHTML}},
	} {
		if err := server.RegisterAll(batch); err == nil {
			t.Fatal("batch collision admitted")
		}
		if len(server.Scenarios()) != 1 || server.ScenarioByPath("/new") != nil {
			t.Fatal("rejected batch partially published")
		}
	}
	for _, view := range []Scenario{*server.ScenarioByID("original"), *server.ScenarioByPath("/original"), server.Scenarios()[0]} {
		view.Expect.Contains[0] = "view mutation"
	}
	actual := server.ScenarioByID("original")
	if actual.Expect.Contains[0] != "original" || actual.Expect.NotContains == nil {
		t.Fatalf("mutable/shape-changing fixture snapshot: %+v", actual)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL("/original"), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || string(data) != "original body" {
		t.Fatalf("rejected registration changed real response: %q %v %v", data, readErr, closeErr)
	}
}

func TestRegistrationConcurrentDuplicateHasOneRealOwner(t *testing.T) {
	server := NewServer()
	defer server.Close()
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for range cap(results) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- server.Register(Scenario{ID: "one", Path: "/one", Kind: KindHTML, HTML: "one"})
		}()
	}
	workers.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 || len(server.Scenarios()) != 1 {
		t.Fatalf("duplicate registration owners=%d", succeeded)
	}
}

func TestFixtureRequestBodyAndConcurrencyAdmissionAtRealHTTPBoundary(t *testing.T) {
	server := NewServer()
	defer server.Close()
	entered := make(chan struct{}, maxFixtureConcurrentRequests)
	release := make(chan struct{})
	if err := server.RegisterHandler("/hold", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		if _, err := io.WriteString(w, "joined"); err != nil {
			return
		}
	})); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, maxFixtureConcurrentRequests)
	for range maxFixtureConcurrentRequests {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL("/hold"), nil)
			if err != nil {
				results <- err
				return
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				results <- err
				return
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != 200 || readErr != nil || closeErr != nil {
				results <- fmt.Errorf("owned request status=%d read=%v close=%v", response.StatusCode, readErr, closeErr)
				return
			}
			results <- nil
		}()
	}
	defer func() {
		close(release)
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Error(err)
			}
		}
	}()
	for range maxFixtureConcurrentRequests {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("actual requests did not occupy bounded capacity")
		}
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL("/hold"), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Error(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("excess concurrency status=%d", response.StatusCode)
	}

	oversized := httptest.NewRequest(http.MethodPost, "/hold", http.NoBody)
	oversized.ContentLength = fixtureUploadMaxBytes + 1
	isolated := NewServer()
	defer isolated.Close()
	called := false
	if err := isolated.RegisterHandler("/hold", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	isolated.handleRequest(recorder, oversized.WithContext(context.Background()))
	if recorder.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("oversized body reached handler: status=%d called=%v", recorder.Code, called)
	}
}

func TestFixtureDeclaredAndChunkedBodyLimitAtRealHTTPBoundary(t *testing.T) {
	server := NewServer()
	defer server.Close()
	if err := server.RegisterHandler("/read", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "observed body limit", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		if _, err := io.WriteString(w, "complete"); err != nil {
			return
		}
	})); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", int(fixtureUploadMaxBytes)+1)
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked-%v", chunked), func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL("/read"), strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if chunked {
				request.ContentLength = -1
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != http.StatusRequestEntityTooLarge || readErr != nil || closeErr != nil {
				t.Fatalf("actual oversized response status=%d read=%v close=%v", response.StatusCode, readErr, closeErr)
			}
		})
	}
}

func TestRegistrationEntryAndPayloadBudgetsCannotPublishPartialState(t *testing.T) {
	server := NewServer()
	defer server.Close()
	body := strings.Repeat("x", maxScenarioBodyBytes)
	if err := server.Register(Scenario{ID: "exact-body", Kind: KindHTML, HTML: body}); err != nil {
		t.Fatalf("exact body bound rejected: %v", err)
	}
	if err := server.Register(Scenario{ID: "over-body", Kind: KindHTML, HTML: body + "x"}); err == nil {
		t.Fatal("excess inline body admitted")
	}
	batch := make([]Scenario, maxRegisteredScenarios)
	for index := range batch {
		batch[index] = Scenario{ID: fmt.Sprintf("case-%04d", index), Kind: KindHTML}
	}
	if err := server.RegisterAll(batch); err == nil {
		t.Fatal("entry bound exceeded")
	}
	if len(server.Scenarios()) != 1 {
		t.Fatal("rejected entry batch partially published")
	}
	for index := 0; index < 6; index++ {
		if err := server.Register(Scenario{ID: fmt.Sprintf("body-%d", index), Kind: KindHTML, HTML: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.Register(Scenario{ID: "aggregate-excess", Kind: KindHTML, HTML: body}); err == nil {
		t.Fatal("aggregate body plus metadata limit bypassed")
	}
	if server.ScenarioByID("aggregate-excess") != nil {
		t.Fatal("aggregate rejection retained entry")
	}
	server.Close()
	if err := server.Register(Scenario{ID: "after-close", Kind: KindHTML}); err == nil {
		t.Fatal("closed owner admitted fixture")
	}
}
