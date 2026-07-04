// Package benchmark implements the Artemis head-to-head benchmark harness
// (TASK-2338). The harness runs Artemis and a competitor engine across a
// versioned scenario matrix, records per-scenario wall time / memory /
// allocations into a committed scorecard, and proves Artemis wins on the
// covered set.
//
// The competitor binary is downloaded at benchmark time into a gitignored
// local path (benchmark/bin/) and is never committed. If the download is
// unavailable the harness reports honestly and still runs the Artemis side.
package benchmark

// ScenarioMatrixVersion is the schema version for the scenario matrix.
// Bump when scenarios are added, removed, or their HTML fixtures change
// in a way that affects comparability.
const ScenarioMatrixVersion = "1.0.0"

// ScenarioKind categorizes a benchmark scenario by the engine surface
// it exercises.
type ScenarioKind string

const (
	KindNavigation  ScenarioKind = "navigation"
	KindScrape      ScenarioKind = "scrape"
	KindDOM         ScenarioKind = "dom"
	KindMarkdown    ScenarioKind = "markdown"
	KindScriptHeavy ScenarioKind = "script-heavy"
)

// Scenario is a single benchmark scenario: an HTML fixture served by
// the httptest server, plus metadata about what engine surface it
// exercises and what extraction is expected.
type Scenario struct {
	// ID is the stable scenario identifier (e.g. "nav-001").
	ID string `json:"id"`
	// Kind is the scenario category.
	Kind ScenarioKind `json:"kind"`
	// Description is a human-readable summary.
	Description string `json:"description"`
	// HTML is the fixture body served by the httptest server.
	HTML string `json:"-"`
	// ScriptCount is the number of <script> tags in the fixture.
	ScriptCount int `json:"scriptCount"`
	// BodyBytes is the fixture body size in bytes.
	BodyBytes int `json:"bodyBytes"`
	// ExpectTitle is the expected <title> text.
	ExpectTitle string `json:"expectTitle"`
	// ExpectLinks is the expected number of <a href> links.
	ExpectLinks int `json:"expectLinks"`
	// ExpectParagraphs is the expected number of <p> elements.
	ExpectParagraphs int `json:"expectParagraphs"`
}

// DefaultScenarios returns the versioned scenario matrix for the
// benchmark harness. The matrix covers five categories: navigation,
// scrape, DOM, markdown, and script-heavy pages. Each scenario is a
// self-contained HTML fixture that the httptest server serves.
func DefaultScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "nav-001",
			Kind:        KindNavigation,
			Description: "Simple navigation page with title and nav links",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Home - Test Site</title>
</head><body>
<nav><a href="/about">About</a> | <a href="/contact">Contact</a> | <a href="/blog">Blog</a></nav>
<main><h1>Welcome</h1><p>This is the home page.</p></main>
<footer><p>(c) 2026 Test Site</p></footer>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Home - Test Site",
			ExpectLinks:      3,
			ExpectParagraphs: 2,
		},
		{
			ID:          "nav-002",
			Kind:        KindNavigation,
			Description: "Navigation with redirect chain and meta refresh",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Redirecting...</title>
<meta http-equiv="refresh" content="0;url=/final">
</head><body>
<p>You are being redirected. <a href="/final">Click here</a> if not redirected.</p>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Redirecting...",
			ExpectLinks:      1,
			ExpectParagraphs: 1,
		},
		{
			ID:          "scr-001",
			Kind:        KindScrape,
			Description: "Article page with structured content and metadata",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Article: The Future of Web Scraping</title>
<meta name="description" content="A deep dive into modern web scraping">
<meta property="og:title" content="The Future of Web Scraping">
<meta property="og:type" content="article">
</head><body>
<article>
<h1>The Future of Web Scraping</h1>
<p>Web scraping has evolved significantly over the past decade.</p>
<p>Modern engines use headless browsers with V8 JavaScript execution.</p>
<p>The key challenge is balancing speed with fidelity.</p>
<p>Artemis solves this with a hybrid renderless + CDP architecture.</p>
<p>Performance benchmarks show sub-millisecond engine cost per page.</p>
</article>
<aside><h3>Related</h3>
<a href="/article/1">Article 1</a>
<a href="/article/2">Article 2</a>
<a href="/article/3">Article 3</a>
</aside>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Article: The Future of Web Scraping",
			ExpectLinks:      3,
			ExpectParagraphs: 6,
		},
		{
			ID:          "scr-002",
			Kind:        KindScrape,
			Description: "Product page with pricing and specs table",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Product: Widget Pro X100</title>
</head><body>
<h1>Widget Pro X100</h1>
<p class="price">$99.99</p>
<table>
<tr><th>Feature</th><th>Value</th></tr>
<tr><td>Weight</td><td>250g</td></tr>
<tr><td>Dimensions</td><td>100x50x20mm</td></tr>
<tr><td>Battery</td><td>2000mAh</td></tr>
<tr><td>Connectivity</td><td>Bluetooth 5.2</td></tr>
</table>
<p>Free shipping on orders over $50.</p>
<a href="/cart">Add to Cart</a>
<a href="/reviews">Reviews</a>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Product: Widget Pro X100",
			ExpectLinks:      2,
			ExpectParagraphs: 2,
		},
		{
			ID:          "dom-001",
			Kind:        KindDOM,
			Description: "DOM-heavy page with many elements and class queries",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>DOM Test Page</title>
</head><body>
<div id="container">
<div class="card item-1"><h2>Card 1</h2><p>Content 1</p></div>
<div class="card item-2"><h2>Card 2</h2><p>Content 2</p></div>
<div class="card item-3"><h2>Card 3</h2><p>Content 3</p></div>
<div class="card item-4"><h2>Card 4</h2><p>Content 4</p></div>
<div class="card item-5"><h2>Card 5</h2><p>Content 5</p></div>
<div class="card item-6"><h2>Card 6</h2><p>Content 6</p></div>
<div class="card item-7"><h2>Card 7</h2><p>Content 7</p></div>
<div class="card item-8"><h2>Card 8</h2><p>Content 8</p></div>
</div>
<ul id="list">
<li>Item A</li><li>Item B</li><li>Item C</li><li>Item D</li><li>Item E</li>
</ul>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "DOM Test Page",
			ExpectLinks:      0,
			ExpectParagraphs: 8,
		},
		{
			ID:          "dom-002",
			Kind:        KindDOM,
			Description: "Nested DOM tree with forms and inputs",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Form Page</title>
</head><body>
<form id="login-form" action="/submit" method="post">
<fieldset>
<legend>Login</legend>
<label for="username">Username:</label>
<input type="text" id="username" name="username">
<label for="password">Password:</label>
<input type="password" id="password" name="password">
<button type="submit">Submit</button>
</fieldset>
</form>
<form id="search-form" action="/search" method="get">
<label for="q">Search:</label>
<input type="search" id="q" name="q" placeholder="Search...">
<button type="submit">Search</button>
</form>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Form Page",
			ExpectLinks:      0,
			ExpectParagraphs: 0,
		},
		{
			ID:          "md-001",
			Kind:        KindMarkdown,
			Description: "Content-rich page for markdown conversion with headings, lists, tables, code",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Markdown Test</title>
</head><body>
<h1>Markdown Conversion Test</h1>
<p>This page tests <strong>bold</strong>, <em>italic</em>, and <a href="https://example.com">links</a>.</p>
<h2>Lists</h2>
<ul><li>First item</li><li>Second item</li><li>Third item</li></ul>
<h2>Code Block</h2>
<pre><code>function hello() {
  console.log("Hello, world!");
}</code></pre>
<h2>Table</h2>
<table>
<tr><th>Name</th><th>Value</th></tr>
<tr><td>Alpha</td><td>1</td></tr>
<tr><td>Beta</td><td>2</td></tr>
</table>
<blockquote>This is a blockquote.</blockquote>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Markdown Test",
			ExpectLinks:      1,
			ExpectParagraphs: 2,
		},
		{
			ID:          "md-002",
			Kind:        KindMarkdown,
			Description: "Complex nested markdown with blockquotes, nested lists, images",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Complex Markdown</title>
</head><body>
<h1>Complex Document</h1>
<p>Intro paragraph with <img src="/img/diagram.png" alt="Diagram"> inline image.</p>
<h2>Section A</h2>
<blockquote>
<p>This is a quoted paragraph.</p>
<p>And a second quoted paragraph.</p>
</blockquote>
<h3>Subsection</h3>
<ol><li>First</li><li>Second</li><li>Third</li></ol>
<h2>Section B</h2>
<p>See <a href="/docs/api">API docs</a> and <a href="/docs/guide">Guide</a>.</p>
</body></html>`,
			ScriptCount:      0,
			ExpectTitle:      "Complex Markdown",
			ExpectLinks:      2,
			ExpectParagraphs: 5,
		},
		{
			ID:          "scr-003",
			Kind:        KindScriptHeavy,
			Description: "Script-heavy page with 10 inline scripts (V8 execution path)",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Script Heavy Page</title>
<script>var config = {debug: false};</script>
<script>var data = [1,2,3,4,5,6,7,8,9,10];</script>
<script>function init() { document.title = "Loaded"; }</script>
<script>var analytics = {page: "home", user: "guest"};</script>
<script>var features = ["search","filter","sort","paginate"];</script>
<script>var state = {loaded: false, items: []};</script>
<script>var utils = {format: function(s) { return s; }};</script>
<script>var cache = new Map();</script>
<script>var observer = {onLoad: init};</script>
<script>document.addEventListener("DOMContentLoaded", init);</script>
</head><body>
<h1>Script Heavy Page</h1>
<p>This page has 10 inline scripts that exercise the V8 execution path.</p>
<div id="app">Loading...</div>
<a href="/page1">Page 1</a>
<a href="/page2">Page 2</a>
</body></html>`,
			ScriptCount:      10,
			ExpectTitle:      "Script Heavy Page",
			ExpectLinks:      2,
			ExpectParagraphs: 1,
		},
		{
			ID:          "scr-004",
			Kind:        KindScriptHeavy,
			Description: "Page with 20 inline scripts and DOM manipulation",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Heavy JS Page</title>
<script>var a = 1;</script>
<script>var b = 2;</script>
<script>var c = 3;</script>
<script>var d = 4;</script>
<script>var e = 5;</script>
<script>var f = 6;</script>
<script>var g = 7;</script>
<script>var h = 8;</script>
<script>var i = 9;</script>
<script>var j = 10;</script>
<script>var k = 11;</script>
<script>var l = 12;</script>
<script>var m = 13;</script>
<script>var n = 14;</script>
<script>var o = 15;</script>
<script>var p = 16;</script>
<script>var q = 17;</script>
<script>var r = 18;</script>
<script>var s = 19;</script>
<script>var t = 20;</script>
</head><body>
<h1>Heavy JS Page</h1>
<p>20 inline scripts exercising V8 context pool reuse.</p>
<ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul>
<a href="/next">Next</a>
</body></html>`,
			ScriptCount:      20,
			ExpectTitle:      "Heavy JS Page",
			ExpectLinks:      1,
			ExpectParagraphs: 1,
		},
	}
}

// ScenarioByID returns the scenario with the given ID, or nil.
func ScenarioByID(id string) *Scenario {
	for _, s := range DefaultScenarios() {
		if s.ID == id {
			return &s
		}
	}
	return nil
}
