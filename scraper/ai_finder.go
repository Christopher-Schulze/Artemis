package scraper

import (
	"context"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// SelectorProposer returns a CSS selector for a natural-language intent.
type SelectorProposer func(ctx context.Context, htmlSnippet, intent string) (string, error)

// AIFinder runs stage-2 LLM selector fallback with validation and cache (spec ss28.12b.3).
type AIFinder struct {
	Proposer SelectorProposer
	Cache    *AdaptiveSelectorCache
	Validate func(doc *webapi.Document, selector string) bool
	MaxTry   int
}

// NewAIFinder creates a finder with defaults.
func NewAIFinder(cache *AdaptiveSelectorCache, proposer SelectorProposer) *AIFinder {
	return &AIFinder{
		Proposer: proposer,
		Cache:    cache,
		Validate: defaultValidateSelector,
		MaxTry:   3,
	}
}

func defaultValidateSelector(doc *webapi.Document, selector string) bool {
	if doc == nil || doc.Root() == nil || strings.TrimSpace(selector) == "" {
		return false
	}
	f := NewFinder()
	_, err := f.Find(doc, selector)
	return err == nil
}

// Find resolves intent via cache, proposer attempts, and validation.
func (a *AIFinder) Find(ctx context.Context, doc *webapi.Document, domain, urlPattern, intent string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("ai finder: nil")
	}
	if a.Cache != nil {
		if e, ok := a.Cache.Get(domain, urlPattern); ok && e.Selector != "" {
			if a.Validate == nil || a.Validate(doc, e.Selector) {
				return e.Selector, nil
			}
		}
	}
	if a.Proposer == nil {
		return "", fmt.Errorf("ai finder: no proposer")
	}
	max := a.MaxTry
	if max <= 0 {
		max = 3
	}
	snippet := ""
	if doc != nil {
		snippet = doc.Title()
	}
	var lastErr error
	for i := 0; i < max; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		q := intent
		if i > 0 {
			q = fmt.Sprintf("%s (attempt %d)", intent, i+1)
		}
		sel, err := a.Proposer(ctx, snippet, q)
		if err != nil {
			lastErr = err
			continue
		}
		sel = strings.TrimSpace(sel)
		if sel == "" {
			lastErr = fmt.Errorf("ai finder: empty selector")
			continue
		}
		if a.Validate != nil && !a.Validate(doc, sel) {
			lastErr = fmt.Errorf("ai finder: invalid selector %q", sel)
			continue
		}
		if a.Cache != nil {
			_ = a.Cache.Put(AdaptiveEntry{
				Domain: domain, URLPattern: urlPattern, Selector: sel, Confidence: 0.85,
			})
		}
		return sel, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ai finder: exhausted attempts")
	}
	return "", lastErr
}
