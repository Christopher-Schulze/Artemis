package actions

import (
	"context"
	"fmt"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

// pageTargetSource adapts the owning bridge context to the tabs projection.
// It never allocates a local tab ID: every entry comes from a live bridge.Page.
type pageTargetSource struct {
	mu       sync.RWMutex
	root     *bridge.Page
	activeID string
}

func (s *pageTargetSource) ListTargets(ctx context.Context) ([]tabs.Target, error) {
	if ctx == nil {
		return nil, fmt.Errorf("actions tabs: context required")
	}
	if s.root == nil {
		return nil, fmt.Errorf("actions tabs: page required")
	}
	pages := s.root.ContextPages()
	targets := make([]tabs.Target, 0, len(pages))
	for _, page := range pages {
		target, err := s.target(ctx, page)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (s *pageTargetSource) CreateTarget(ctx context.Context, url string) (tabs.Target, error) {
	if ctx == nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: context required")
	}
	if s.root == nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: page required")
	}
	page, err := s.root.NewSibling(ctx, url)
	if err != nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: create page: %w", err)
	}
	return s.target(ctx, page)
}

func (s *pageTargetSource) ActivateTarget(ctx context.Context, id string) error {
	page, err := s.page(id)
	if err != nil {
		return err
	}
	if err := page.Activate(ctx); err != nil {
		return fmt.Errorf("actions tabs: activate page: %w", err)
	}
	s.mu.Lock()
	s.activeID = id
	s.mu.Unlock()
	return nil
}

func (s *pageTargetSource) CloseTarget(ctx context.Context, id string) error {
	page, err := s.page(id)
	if err != nil {
		return err
	}
	if err := page.Close(); err != nil {
		return fmt.Errorf("actions tabs: close page: %w", err)
	}
	s.mu.Lock()
	if s.activeID == id {
		s.activeID = ""
	}
	s.mu.Unlock()
	return nil
}

func (s *pageTargetSource) page(id string) (*bridge.Page, error) {
	if s.root == nil {
		return nil, fmt.Errorf("actions tabs: page required")
	}
	for _, page := range s.root.ContextPages() {
		if page.TargetID() == id {
			return page, nil
		}
	}
	return nil, fmt.Errorf("actions tabs: target %s not found", id)
}

func (s *pageTargetSource) target(ctx context.Context, page *bridge.Page) (tabs.Target, error) {
	if page == nil || page.TargetID() == "" {
		return tabs.Target{}, fmt.Errorf("actions tabs: page target ID required")
	}
	target := tabs.Target{ID: page.TargetID(), ContextID: page.BrowserContextID(), State: string(page.State())}
	s.mu.RLock()
	activeID := s.activeID
	s.mu.RUnlock()
	if target.State == string(bridge.TargetStateAttached) && target.ID == activeID {
		target.State = "active"
	}
	if page.State() != bridge.TargetStateAttached {
		return target, nil
	}
	var result struct {
		Result struct {
			Value struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": "({url:location.href,title:document.title})", "returnByValue": true,
	}, &result); err != nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: inspect target %s: %w", page.TargetID(), err)
	}
	target.URL = result.Result.Value.URL
	target.Title = result.Result.Value.Title
	return target, nil
}
