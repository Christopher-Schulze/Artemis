package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func benchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Bench</title></head><body><h1>Bench</h1><p>Hello <b>world</b>.</p><button id="b">Press</button><script>document.getElementById('b').addEventListener('click', () => { document.getElementById('b').textContent = 'clicked'; });</script></body></html>`)
	}))
}

func BenchmarkFetch(b *testing.B) {
	srv := benchServer()
	defer srv.Close()
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := eng.Fetch(ctx, srv.URL, FetchOpts{})
		if err != nil {
			b.Fatal(err)
		}
		p.Close()
	}
}

func BenchmarkFetchRunScripts(b *testing.B) {
	srv := benchServer()
	defer srv.Close()
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
		if err != nil {
			b.Fatal(err)
		}
		p.Close()
	}
}

func BenchmarkEval(b *testing.B) {
	srv := benchServer()
	defer srv.Close()
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()
	page, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		b.Fatal(err)
	}
	defer page.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := page.Eval(ctx, `document.title`); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClickRoundTrip(b *testing.B) {
	srv := benchServer()
	defer srv.Close()
	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()
	page, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		b.Fatal(err)
	}
	defer page.Close()
	btn, _ := page.Document().QuerySelector("#b")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := page.Click(ctx, btn); err != nil {
			b.Fatal(err)
		}
	}
}
