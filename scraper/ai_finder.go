package scraper

import (
	"context"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// SelectorProposer returns a CSS selector from bounded redacted DOM evidence.
// The embedding host owns inference privacy and provider admission.
type SelectorProposer func(ctx context.Context, pageEvidence, intent string) (string, error)

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
	if doc == nil || doc.Root() == nil || strings.TrimSpace(selector) == "" || len(selector) > 1024 {
		return false
	}
	nodes, err := doc.QuerySelectorAll(selector)
	return err == nil && len(nodes) == 1
}

// Find resolves intent via cache, proposer attempts, and validation.
func (a *AIFinder) Find(ctx context.Context, doc *webapi.Document, domain, urlPattern, intent string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("ai finder: nil")
	}
	if ctx == nil || doc == nil || doc.Root() == nil || strings.TrimSpace(intent) == "" {
		return "", fmt.Errorf("ai finder: context, document and structural intent required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	pattern, err := selectorCachePattern(domain, urlPattern, doc.URL(), intent)
	if err != nil {
		return "", err
	}
	if a.Cache != nil {
		e, ok, err := a.Cache.LookupContext(ctx, domain, pattern)
		if err != nil {
			return "", err
		}
		if ok && e.Selector != "" {
			if a.validSelector(doc, e.Selector) {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return e.Selector, nil
			}
			if err := a.Cache.DeleteContext(ctx, domain, pattern); err != nil {
				return "", fmt.Errorf("ai finder: invalidate stale selector: %w", err)
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
	max = min(max, 3)
	snippet, err := selectorPageEvidence(ctx, doc)
	if err != nil {
		return "", err
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
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		if err != nil {
			lastErr = err
			continue
		}
		sel = strings.TrimSpace(sel)
		if sel == "" {
			lastErr = fmt.Errorf("ai finder: empty selector")
			continue
		}
		if !a.validSelector(doc, sel) {
			lastErr = fmt.Errorf("ai finder: selector is invalid, missing or ambiguous")
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if a.Cache != nil {
			if err := a.Cache.PutContext(ctx, AdaptiveEntry{
				Domain: domain, URLPattern: pattern, Selector: sel,
			}); err != nil {
				return "", fmt.Errorf("ai finder: cache selector: %w", err)
			}
		}
		return sel, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ai finder: exhausted attempts")
	}
	return "", lastErr
}

func (a *AIFinder) validSelector(doc *webapi.Document, selector string) bool {
	return defaultValidateSelector(doc, selector) && (a.Validate == nil || a.Validate(doc, selector))
}
