package scraper

import "testing"

func BenchmarkDetectEscalationSignals(b *testing.B) {
	// Simulate a realistic content page (~4KB)
	body := []byte(`<!doctype html><html lang="en"><head>
<meta charset="utf-8"><title>Article</title>
<meta name="description" content="A test article">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article"}</script>
</head><body><nav><a href="/">Home</a></nav>
<main><article><h1>Title</h1>
<p>Paragraph 1 with <a href="/x">link</a>.</p>
<p>Paragraph 2.</p><p>Paragraph 3.</p>
</article></main><footer>(c) 2026</footer></body></html>`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectEscalationSignals(200, body)
	}
}

func BenchmarkDetectEscalationSignalsSPA(b *testing.B) {
	// Simulate a SPA shell (~500 bytes, many scripts)
	body := []byte(`<!doctype html><html><head>
<script src="/vendor.js"></script><script src="/app.js"></script>
<script src="/runtime.js"></script><script src="/polyfills.js"></script>
<script src="/main.js"></script><script src="/styles.js"></script>
<script src="/chunks/chunk1.js"></script><script src="/chunks/chunk2.js"></script>
<script src="/chunks/chunk3.js"></script><script src="/chunks/chunk4.js"></script>
<script src="/chunks/chunk5.js"></script><script src="/chunks/chunk6.js"></script>
<script src="/chunks/chunk7.js"></script><script src="/chunks/chunk8.js"></script>
<script src="/chunks/chunk9.js"></script><script src="/chunks/chunk10.js"></script>
<script src="/chunks/chunk11.js"></script><script src="/chunks/chunk12.js"></script>
<script src="/chunks/chunk13.js"></script><script src="/chunks/chunk14.js"></script>
<script src="/chunks/chunk15.js"></script><script src="/chunks/chunk16.js"></script>
</head><body><div id="root"></div></body></html>`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectEscalationSignals(200, body)
	}
}

func BenchmarkShouldEscalate(b *testing.B) {
	sig := EscalationSignals{
		StatusCode:     200,
		BodyLen:        4096,
		BodyHasContent: true,
		ScriptCount:    3,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ShouldEscalate(sig)
	}
}
