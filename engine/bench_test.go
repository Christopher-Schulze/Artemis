package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func benchServer(b *testing.B) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, `<!doctype html><html><head><title>Bench</title></head><body><h1>Bench</h1><p>Hello <b>world</b>.</p><button id="b">Press</button><script>document.getElementById('b').addEventListener('click', () => { document.getElementById('b').textContent = 'clicked'; });</script></body></html>`); err != nil {
			b.Errorf("write benchmark response: %v", err)
		}
	}))
}

func BenchmarkFetch(b *testing.B) {
	srv := benchServer(b)
	defer srv.Close()
	eng, err := New(testConfig(srv))
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := eng.Fetch(ctx, srv.URL, FetchOpts{})
		if err != nil {
			b.Fatal(err)
		}
		if err := p.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFetchRunScripts(b *testing.B) {
	srv := benchServer(b)
	defer srv.Close()
	eng, err := New(testConfig(srv))
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
		if err != nil {
			b.Fatal(err)
		}
		if err := p.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEval(b *testing.B) {
	srv := benchServer(b)
	defer srv.Close()
	eng, err := New(testConfig(srv))
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	ctx := context.Background()
	page, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "page", page.Close)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := page.Eval(ctx, `document.title`); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClickRoundTrip(b *testing.B) {
	srv := benchServer(b)
	defer srv.Close()
	eng, err := New(testConfig(srv))
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "engine", eng.Close)
	ctx := context.Background()
	page, err := eng.Fetch(ctx, srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		b.Fatal(err)
	}
	defer closeTestResource(b, "page", page.Close)
	btn, err := page.Document().QuerySelector("#b")
	if err != nil {
		b.Fatal(err)
	}
	if btn == nil {
		b.Fatal("benchmark button not found")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := page.Click(ctx, btn); err != nil {
			b.Fatal(err)
		}
	}
}
