package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkEngineFetchPerf measures the cost of a renderless (no JS
// execution) Fetch through the Engine using an OnRequest mock that
// short-circuits the network call. This isolates the parse + JS context
// build path that the renderless claim targets.
func BenchmarkEngineFetchPerf(b *testing.B) {
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	body := []byte("<html><head><title>perf</title></head><body><p>hello</p></body></html>")
	opts := FetchOpts{
		OnRequest: func(*RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{Status: 200, Body: body, FinalURL: "https://example.invalid/perf"}, nil
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := eng.Fetch(context.Background(), "https://example.invalid/perf", opts)
		if err != nil {
			b.Fatal(err)
		}
		if page.StatusCode() != 200 {
			b.Fatalf("status=%d", page.StatusCode())
		}
		if err := page.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEngineFetchPerfBaseline runs the same Fetch path but with
// RunScripts enabled, which forces the script walker to traverse the
// document and attempt execution. This is the slower baseline against
// which the renderless fast path is compared.
func BenchmarkEngineFetchPerfBaseline(b *testing.B) {
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	body := []byte("<html><head><title>perf</title></head><body><p>hello</p></body></html>")
	opts := FetchOpts{
		RunScripts: true,
		OnRequest: func(*RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{Status: 200, Body: body, FinalURL: "https://example.invalid/perf"}, nil
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := eng.Fetch(context.Background(), "https://example.invalid/perf", opts)
		if err != nil {
			b.Fatal(err)
		}
		if page.StatusCode() != 200 {
			b.Fatalf("status=%d", page.StatusCode())
		}
		if err := page.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// TestEngineFetchPerfCorrectness verifies the renderless Fetch returns
// a parsed Page whose accessors reflect the mocked response, proving the
// performance benchmark exercises real parsing rather than a no-op.
func TestEngineFetchPerfCorrectness(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", eng.Close)
	body := []byte("<html><head><title>renderless</title></head><body><a href='/x'>link</a></body></html>")
	opts := FetchOpts{
		OnRequest: func(*RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{Status: 200, Body: body, FinalURL: "https://example.invalid/p"}, nil
		},
	}
	page, err := eng.Fetch(context.Background(), "https://example.invalid/p", opts)
	if err != nil {
		t.Fatal(err)
	}
	if page.StatusCode() != 200 {
		t.Fatalf("status=%d", page.StatusCode())
	}
	if page.URL() != "https://example.invalid/p" {
		t.Fatalf("url=%q", page.URL())
	}
	if page.Title() != "renderless" {
		t.Fatalf("title=%q", page.Title())
	}
	links := page.Links()
	if len(links) != 1 || !strings.HasSuffix(links[0].Href, "/x") {
		t.Fatalf("links=%+v", links)
	}
	fmt.Printf("renderless_fetch_status=%d parsed_title_len=%d link_count=%d\n",
		page.StatusCode(), len(page.Title()), len(links))
}
