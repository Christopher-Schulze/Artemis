package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Finder is an adaptive element locator that tries cheap deterministic
// strategies before falling back to slower heuristics.
type Finder struct {
	cache *finderCache
}

// NewFinder creates a Finder with an in-memory LRU cache.
func NewFinder() *Finder {
	return &Finder{cache: newFinderCache(128)}
}

// Result describes what was found and how.
type Result struct {
	Node       *webapi.Node
	Strategy   string  // css, xpath, text, attr, heuristic
	Confidence float64 // 0..1
}

// Find tries to locate an element matching the intent.  The query may be a
// CSS selector, an XPath-like expression, or a human description.
// Stage order: CSS → Text → Attribute → Structural heuristic.
func (f *Finder) Find(doc *webapi.Document, query string) (Result, error) {
	if doc == nil || doc.Root() == nil {
		return Result{}, fmt.Errorf("nil document")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, fmt.Errorf("empty query")
	}

	// 1. Cache check
	if r, ok := f.cache.get(doc.URL(), query); ok {
		return r, nil
	}

	// 2. CSS selector stage
	if r, ok := f.tryCSS(doc, query); ok {
		f.cache.set(doc.URL(), query, r)
		return r, nil
	}

	// 3. XPath-like shorthand stage  (e.g. "//button[@id='ok']")
	if r, ok := f.tryXPath(doc, query); ok {
		f.cache.set(doc.URL(), query, r)
		return r, nil
	}

	// 4. Text content heuristic
	if r, ok := f.tryText(doc, query); ok {
		f.cache.set(doc.URL(), query, r)
		return r, nil
	}

	// 5. Attribute heuristic
	if r, ok := f.tryAttribute(doc, query); ok {
		f.cache.set(doc.URL(), query, r)
		return r, nil
	}

	// 6. Structural heuristic (tag + near text)
	if r, ok := f.tryStructural(doc, query); ok {
		f.cache.set(doc.URL(), query, r)
		return r, nil
	}

	return Result{}, fmt.Errorf("no element matched query %q", query)
}

func (f *Finder) tryCSS(doc *webapi.Document, sel string) (Result, bool) {
	s, err := cascadia.Parse(sel)
	if err != nil {
		return Result{}, false
	}
	root := doc.RawRoot()
	if root == nil {
		return Result{}, false
	}
	match := cascadia.Query(root, s)
	if match == nil {
		return Result{}, false
	}
	return Result{Node: webapi.Wrap(match), Strategy: "css", Confidence: 1.0}, true
}

func (f *Finder) tryXPath(doc *webapi.Document, expr string) (Result, bool) {
	// Minimal XPath shorthand: //tag[@attr='value'] or //tag[text()='value']
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "//") {
		return Result{}, false
	}
	expr = expr[2:]

	// Parse tag
	var tag string
	if idx := strings.IndexAny(expr, "[@"); idx >= 0 {
		tag = expr[:idx]
		expr = expr[idx:]
	} else {
		tag = expr
		expr = ""
	}

	var attr, val string
	if strings.HasPrefix(expr, "[@") && strings.HasSuffix(expr, "]") {
		inner := expr[2 : len(expr)-1]
		parts := strings.SplitN(inner, "=", 2)
		if len(parts) == 2 {
			attr = strings.TrimSpace(parts[0])
			val = strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		}
	}

	var found *html.Node
	webapi.Walk(doc.Root(), func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement {
			return webapi.WalkContinue
		}
		if tag != "" && n.Tag() != tag {
			return webapi.WalkContinue
		}
		if attr != "" {
			v, _ := n.Attr(attr)
			if v != val {
				return webapi.WalkContinue
			}
		}
		found = n.Raw()
		return webapi.WalkStop
	})
	if found != nil {
		return Result{Node: webapi.Wrap(found), Strategy: "xpath", Confidence: 0.95}, true
	}
	return Result{}, false
}

var containerTags = map[string]bool{
	"html": true, "head": true, "body": true, "div": true, "section": true,
	"article": true, "main": true, "nav": true, "header": true, "footer": true,
	"aside": true, "form": true, "table": true, "tbody": true, "thead": true,
	"tfoot": true, "tr": true, "ul": true, "ol": true, "dl": true,
}

func (f *Finder) tryText(doc *webapi.Document, q string) (Result, bool) {
	qLower := strings.ToLower(q)
	var best *html.Node
	var bestScore int
	webapi.Walk(doc.Root(), func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement {
			return webapi.WalkContinue
		}
		if containerTags[n.Tag()] {
			return webapi.WalkContinue
		}
		text := strings.ToLower(strings.TrimSpace(n.Text()))
		if text == "" {
			return webapi.WalkContinue
		}
		score := textMatchScore(text, qLower)
		if score > bestScore {
			bestScore = score
			best = n.Raw()
		}
		return webapi.WalkContinue
	})
	if best != nil && bestScore > 50 {
		conf := float64(bestScore) / 100.0
		if conf > 1.0 {
			conf = 1.0
		}
		return Result{Node: webapi.Wrap(best), Strategy: "text", Confidence: conf}, true
	}
	return Result{}, false
}

func (f *Finder) tryAttribute(doc *webapi.Document, q string) (Result, bool) {
	qLower := strings.ToLower(q)
	var best *html.Node
	var bestScore int
	webapi.Walk(doc.Root(), func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement {
			return webapi.WalkContinue
		}
		for k, v := range n.Attrs() {
			keyLow := strings.ToLower(k)
			valLow := strings.ToLower(v)
			score := textMatchScore(keyLow+"="+valLow, qLower)
			if score > bestScore {
				bestScore = score
				best = n.Raw()
			}
		}
		return webapi.WalkContinue
	})
	if best != nil && bestScore > 50 {
		conf := float64(bestScore) / 100.0
		if conf > 1.0 {
			conf = 1.0
		}
		return Result{Node: webapi.Wrap(best), Strategy: "attr", Confidence: conf}, true
	}
	return Result{}, false
}

func (f *Finder) tryStructural(doc *webapi.Document, q string) (Result, bool) {
	// Very simple structural heuristic: find a tag word in the query,
	// then look for the nearest text word inside that tag family.
	words := strings.Fields(strings.ToLower(q))
	var tagHint string
	for _, w := range words {
		w = strings.TrimSuffix(w, ".")
		w = strings.TrimSuffix(w, ",")
		if isCommonTag(w) {
			tagHint = w
			break
		}
	}
	if tagHint == "" {
		return Result{}, false
	}
	var best *html.Node
	var bestScore int
	webapi.Walk(doc.Root(), func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement || n.Tag() != tagHint {
			return webapi.WalkContinue
		}
		text := strings.ToLower(strings.TrimSpace(n.Text()))
		score := textMatchScore(text, strings.ToLower(q))
		if score > bestScore {
			bestScore = score
			best = n.Raw()
		}
		return webapi.WalkContinue
	})
	if best != nil && bestScore > 30 {
		conf := float64(bestScore) / 100.0
		if conf > 0.9 {
			conf = 0.9
		}
		return Result{Node: webapi.Wrap(best), Strategy: "heuristic", Confidence: conf}, true
	}
	return Result{}, false
}

func textMatchScore(text, query string) int {
	// Simple token overlap scoring
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return 0
	}
	var hits int
	for _, tok := range tokens {
		if strings.Contains(text, tok) {
			hits++
		}
	}
	return hits * 100 / len(tokens)
}

func tokenize(s string) []string {
	re := regexp.MustCompile(`[a-z0-9]+`)
	return re.FindAllString(strings.ToLower(s), -1)
}

func isCommonTag(s string) bool {
	switch s {
	case "a", "button", "input", "form", "div", "span", "label", "select",
		"textarea", "h1", "h2", "h3", "h4", "h5", "h6", "p", "li", "table",
		"tr", "td", "th", "nav", "header", "footer", "section", "article":
		return true
	}
	return false
}

// --- cache ---

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

type finderCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	max     int
}

func newFinderCache(max int) *finderCache {
	return &finderCache{entries: make(map[string]cacheEntry), max: max}
}

func (c *finderCache) key(url, query string) string {
	return url + "||" + query
}

func (c *finderCache) get(url, query string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[c.key(url, query)]
	if !ok || time.Now().After(e.expiresAt) {
		return Result{}, false
	}
	return e.result, true
}

func (c *finderCache) set(url, query string, r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		// naive eviction: clear half
		i := 0
		for k := range c.entries {
			delete(c.entries, k)
			i++
			if i >= c.max/2 {
				break
			}
		}
	}
	c.entries[c.key(url, query)] = cacheEntry{result: r, expiresAt: time.Now().Add(5 * time.Minute)}
}
