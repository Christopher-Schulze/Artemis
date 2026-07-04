package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// realisticHTML generates a page roughly representative of a modern
// content site: head metadata, JSON-LD, navbar, article body with
// inline scripts that build a list, and a footer.
func realisticHTML(idx int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head>
<meta charset="utf-8">
<title>Article %d</title>
<meta property="og:title" content="A %d">
<meta property="og:image" content="https://e.test/i%d.png">
<meta name="description" content="Article %d description">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"A %d","datePublished":"2026-05-01"}</script>
</head><body>
<nav><a href="/">Home</a><a href="/articles">Articles</a></nav>
<main>
<article>
<h1>Article %d Heading</h1>
<p>Lorem ipsum dolor sit amet, <b>consectetur</b> adipiscing elit. <a href="/related-%d">Related</a>.</p>
<h2>Section A</h2>`, idx, idx, idx, idx, idx, idx, idx)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, `<p>Paragraph %d-%d with <i>emphasis</i> and <a href="/x%d">link</a>.</p>`, idx, i, i)
	}
	fmt.Fprintf(&b, `<h2>Section B</h2><ul>`)
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, `<li>Item %d</li>`, i)
	}
	b.WriteString(`</ul>`)
	b.WriteString(`<script>(function(){
		const m = document.querySelector('main');
		const ts = document.createElement('p');
		ts.textContent = 'Built at ' + Date.now();
		m.appendChild(ts);
		const list = document.createElement('ul');
		['a','b','c','d','e'].forEach(x => {
			const li = document.createElement('li');
			li.textContent = 'js-' + x;
			list.appendChild(li);
		});
		m.appendChild(list);
	})();</script>`)
	b.WriteString(`</article></main><footer><p>(c) 2026</p></footer></body></html>`)
	return b.String()
}

// BenchmarkEndToEnd100Pages mirrors a published headless-browser demo: fetch
// 100 pages and dump markdown each. A comparable headless browser reports 5s
// wall-clock (50ms/page) on AWS m5.large. We use a local httptest server so
// network latency is near zero, isolating engine cost.
func BenchmarkEndToEnd100Pages(b *testing.B) {
	mux := http.NewServeMux()
	for i := 0; i < 200; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/p/%d", i), func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, realisticHTML(i))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			url := fmt.Sprintf("%s/p/%d", srv.URL, j)
			page, err := eng.Fetch(ctx, url, FetchOpts{RunScripts: true})
			if err != nil {
				b.Fatal(err)
			}
			_ = page.Markdown()
			_ = page.Close()
		}
	}
}

// BenchmarkEndToEnd100PagesPooled exercises the v8.Context pool path:
// pages reuse the underlying v8.Context across requests via JS-side
// reset, skipping ~30% of NewContext CPU cost.
func BenchmarkEndToEnd100PagesPooled(b *testing.B) {
	mux := http.NewServeMux()
	for i := 0; i < 200; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/p/%d", i), func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, realisticHTML(i))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{JSContextPoolSize: 8})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			url := fmt.Sprintf("%s/p/%d", srv.URL, j)
			page, err := eng.Fetch(ctx, url, FetchOpts{RunScripts: true})
			if err != nil {
				b.Fatal(err)
			}
			_ = page.Markdown()
			_ = page.Close()
		}
	}
}

// BenchmarkEndToEnd100PagesPooledWarm pre-warms the v8.Context pool so
// the first page hits the fast path immediately. Eliminates the
// first-page cold-build cost from the per-iteration measurement.
func BenchmarkEndToEnd100PagesPooledWarm(b *testing.B) {
	mux := http.NewServeMux()
	for i := 0; i < 200; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/p/%d", i), func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, realisticHTML(i))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{JSContextPoolSize: 8, JSContextPoolWarm: true})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			url := fmt.Sprintf("%s/p/%d", srv.URL, j)
			page, err := eng.Fetch(ctx, url, FetchOpts{RunScripts: true})
			if err != nil {
				b.Fatal(err)
			}
			_ = page.Markdown()
			_ = page.Close()
		}
	}
}

func BenchmarkEndToEnd100PagesNoScripts(b *testing.B) {
	mux := http.NewServeMux()
	for i := 0; i < 200; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/p/%d", i), func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, realisticHTML(i))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			url := fmt.Sprintf("%s/p/%d", srv.URL, j)
			page, err := eng.Fetch(ctx, url, FetchOpts{})
			if err != nil {
				b.Fatal(err)
			}
			_ = page.Markdown()
			_ = page.Close()
		}
	}
}
