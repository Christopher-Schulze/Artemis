package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// TestMarkdownPoolConcurrent verifies the mdConverterPool and builderPool
// are race-free under concurrent Markdown() calls. Each goroutine builds
// a distinct document and checks the output contains expected text.
func TestMarkdownPoolConcurrent(t *testing.T) {
	const goroutines = 16
	const iterations = 50

	docs := make([]*webapi.Document, goroutines)
	for i := range docs {
		html := fmt.Sprintf(`<html><body><h1>Title %d</h1>
<p>Paragraph with <a href="https://example.com/%d">link %d</a>.</p>
<ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul>
<blockquote>Quote text %d</blockquote>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
<pre><code>code block %d</code></pre>
</body></html>`, i, i, i, i, i)
		d, err := parser.ParseHTML(strings.NewReader(html), fmt.Sprintf("https://example.com/%d", i))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		docs[i] = d
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				md := Markdown(docs[idx])
				expected := fmt.Sprintf("Title %d", idx)
				if !strings.Contains(md, expected) {
					t.Errorf("goroutine %d iter %d: markdown missing %q", idx, iter, expected)
				}
			}
		}(g)
	}
	wg.Wait()
}
