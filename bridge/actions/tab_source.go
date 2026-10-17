package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

var _ tabs.TargetRecycler = (*pageTargetSource)(nil)

// pageTargetSource adapts the owning bridge context to the tabs projection.
// It never allocates a local tab ID: every entry comes from a live bridge.Page.
type pageTargetSource struct {
	mu       sync.RWMutex
	root     *bridge.Page
	activeID string
	userID   string
}

func (s *pageTargetSource) ListTargets(ctx context.Context) ([]tabs.Target, error) {
	if ctx == nil {
		return nil, fmt.Errorf("actions tabs: context required")
	}
	if s.root == nil {
		return nil, fmt.Errorf("actions tabs: page required")
	}
	pages, err := s.root.ContextPagesContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("actions tabs: list live pages: %w", err)
	}
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
		return targetCreateFailure(err)
	}
	target, err := s.target(ctx, page)
	if err != nil {
		createdAt, lastActive := page.LifecycleTimes()
		partial := tabs.Target{ID: page.TargetID(), ContextID: page.BrowserContextID(), State: string(page.State()), CreatedAt: createdAt, LastActive: lastActive}
		return partial, &tabs.TargetCreateError{TargetID: partial.ID, Err: fmt.Errorf("actions tabs: inspect created target: %w", err)}
	}
	return target, nil
}

func targetCreateFailure(err error) (tabs.Target, error) {
	var targetErr *bridge.TargetError
	if errors.As(err, &targetErr) && (targetErr.TargetID != "" || targetErr.OutcomeUnknown) {
		return tabs.Target{ID: targetErr.TargetID, State: string(targetErr.State)}, &tabs.TargetCreateError{
			TargetID: targetErr.TargetID, Compensated: targetErr.Compensated, OutcomeUnknown: targetErr.OutcomeUnknown,
			Err: fmt.Errorf("actions tabs: create page: %w", err),
		}
	}
	return tabs.Target{}, fmt.Errorf("actions tabs: create page: %w", err)
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
	if s.root == nil {
		return fmt.Errorf("actions tabs: page required")
	}
	if err := s.root.CloseTargetInContext(ctx, id); err != nil {
		return fmt.Errorf("actions tabs: close page: %w", err)
	}
	s.mu.Lock()
	if s.activeID == id {
		s.activeID = ""
	}
	s.mu.Unlock()
	return nil
}

// RecycleTarget clears the target's origin cookies and storage and navigates
// it to about:blank (spec L4522); the browser keeps DNS, TLS and HTTP/2
// state, so reuse costs no new connection setup.
func (s *pageTargetSource) RecycleTarget(ctx context.Context, id string) error {
	page, err := s.page(id)
	if err != nil {
		return err
	}
	var result runtimeCallResult
	if err := page.Call(ctx, "Runtime.evaluate", runtimeEvaluateParams{Expression: "location.origin", ReturnByValue: true}, &result); err != nil {
		return fmt.Errorf("actions tabs: read origin of %s: %w", id, err)
	}
	var origin string
	if err := result.valueInto(&origin); err != nil {
		return fmt.Errorf("actions tabs: decode origin of %s: %w", id, err)
	}
	if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
		if err := page.Call(ctx, "Storage.clearDataForOrigin", clearOriginDataParams{Origin: origin, StorageTypes: "cookies,local_storage,session_storage,indexeddb,cache_storage,service_workers"}, nil); err != nil {
			return fmt.Errorf("actions tabs: clear origin data of %s: %w", id, err)
		}
	}
	// about:blank loads no network resource, so the policy-checked
	// navigation path (which admits only network URLs) is not needed.
	var navigated struct {
		ErrorText string `json:"errorText"`
	}
	if err := page.Call(ctx, "Page.navigate", blankNavigateParams{URL: "about:blank"}, &navigated); err != nil {
		return fmt.Errorf("actions tabs: blank %s: %w", id, err)
	}
	if navigated.ErrorText != "" {
		return fmt.Errorf("actions tabs: blank %s: %s", id, navigated.ErrorText)
	}
	return nil
}

type blankNavigateParams struct {
	URL string `json:"url"`
}

type clearOriginDataParams struct {
	Origin       string `json:"origin"`
	StorageTypes string `json:"storageTypes"`
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
	createdAt, lastActive := page.LifecycleTimes()
	target := tabs.Target{ID: page.TargetID(), UserID: s.userID, ContextID: page.BrowserContextID(), State: string(page.State()), CreatedAt: createdAt, LastActive: lastActive}
	process := page.ProcessInfo()
	target.OwnerMetadata = tabs.TabOwnerMetadata{ProcessID: process.ID, OwnerRef: string(process.Owner), PriorityLane: process.Spec.PriorityLane, MailboxCap: process.Spec.MailboxCap}
	s.mu.RLock()
	activeID := s.activeID
	s.mu.RUnlock()
	if target.State == string(bridge.TargetStateAttached) && target.ID == activeID {
		target.State = "active"
	}
	if page.State() != bridge.TargetStateAttached {
		return target, nil
	}
	params := runtimeEvaluateParams{Expression: "({url:location.href,title:document.title})", ReturnByValue: true}
	var result runtimeCallResult
	if err := page.Call(ctx, "Runtime.evaluate", params, &result); err != nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: inspect target %s: %w", page.TargetID(), err)
	}
	var metadata targetMetadata
	if err := result.valueInto(&metadata); err != nil {
		return tabs.Target{}, fmt.Errorf("actions tabs: decode target %s: %w", page.TargetID(), err)
	}
	target.URL = metadata.URL
	target.Title = metadata.Title
	target.MetadataKnown = true
	return target, nil
}
