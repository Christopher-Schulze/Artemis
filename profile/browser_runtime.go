package profile

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

type BrowserRuntime struct {
	mu       sync.Mutex
	manager  *RuntimeManager
	sessions map[SessionID]*ownedBrowserSession
}

type ownedBrowserSession struct {
	browser *bridge.ChromiumBrowser
	context *bridge.BrowserContext
	pages   map[PageID]*bridge.Page
}

func NewBrowserRuntime(manager *RuntimeManager) (*BrowserRuntime, error) {
	if manager == nil {
		return nil, errors.New("browser runtime: session manager required")
	}
	return &BrowserRuntime{manager: manager, sessions: make(map[SessionID]*ownedBrowserSession)}, nil
}

func (r *BrowserRuntime) Open(ctx context.Context, request OpenSessionRequest, launch browserprocess.LaunchConfig) (*RuntimeSession, error) {
	session, err := r.manager.Open(ctx, request)
	if err != nil {
		return nil, err
	}
	if session.Class != ProfileAttached {
		launch.UserDataDir = session.DataDir
	}
	browser, err := bridge.LaunchChromium(ctx, launch)
	if err != nil {
		_ = r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: launch: %w", err)
	}
	var browserContext *bridge.BrowserContext
	if session.Class == ProfilePersistent {
		browserContext, err = browser.DefaultContext()
	} else {
		browserContext, err = browser.NewContext(ctx)
	}
	if err != nil {
		_ = browser.Close()
		_ = r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: context: %w", err)
	}
	r.mu.Lock()
	r.sessions[session.ID] = &ownedBrowserSession{browser: browser, context: browserContext, pages: make(map[PageID]*bridge.Page)}
	r.mu.Unlock()
	return session, nil
}

func (r *BrowserRuntime) Attach(ctx context.Context, request OpenSessionRequest, endpoint string) (*RuntimeSession, error) {
	request.Class = ProfileAttached
	session, err := r.manager.Open(ctx, request)
	if err != nil {
		return nil, err
	}
	browser, err := bridge.ConnectChromium(ctx, endpoint)
	if err != nil {
		_ = r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: attach: %w", err)
	}
	browserContext, err := browser.DefaultContext()
	if err != nil {
		_ = browser.Close()
		_ = r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, err
	}
	r.mu.Lock()
	r.sessions[session.ID] = &ownedBrowserSession{browser: browser, context: browserContext, pages: make(map[PageID]*bridge.Page)}
	r.mu.Unlock()
	return session, nil
}

func (r *BrowserRuntime) NewPage(ctx context.Context, sessionID SessionID, owner, initialURL string) (PageID, *bridge.Page, error) {
	r.mu.Lock()
	owned := r.sessions[sessionID]
	r.mu.Unlock()
	if owned == nil {
		return "", nil, &RuntimeError{Class: FailureNotFound, Op: "new page", Msg: "browser session not active"}
	}
	if _, err := r.manager.Get(sessionID, owner); err != nil {
		return "", nil, err
	}
	page, err := owned.context.NewPage(ctx, initialURL)
	if err != nil {
		return "", nil, err
	}
	pageID, err := r.manager.RegisterPage(sessionID, owner, page.TargetID())
	if err != nil {
		_ = page.Close()
		return "", nil, err
	}
	r.mu.Lock()
	owned.pages[pageID] = page
	r.mu.Unlock()
	return pageID, page, nil
}

func (r *BrowserRuntime) Page(sessionID SessionID, pageID PageID, owner string) (*bridge.Page, error) {
	if _, err := r.manager.ResolvePage(sessionID, pageID, owner); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owned := r.sessions[sessionID]
	if owned == nil || owned.pages[pageID] == nil {
		return nil, &RuntimeError{Class: FailureNotFound, Op: "page", Msg: "page not active"}
	}
	return owned.pages[pageID], nil
}

func (r *BrowserRuntime) ClosePage(sessionID SessionID, pageID PageID, owner string) error {
	page, err := r.Page(sessionID, pageID, owner)
	if err != nil {
		return err
	}
	if err := page.Close(); err != nil {
		return err
	}
	r.mu.Lock()
	if owned := r.sessions[sessionID]; owned != nil {
		delete(owned.pages, pageID)
	}
	r.mu.Unlock()
	return r.manager.ClosePage(sessionID, pageID, owner)
}

func (r *BrowserRuntime) Close(ctx context.Context, sessionID SessionID, owner string) error {
	if _, err := r.manager.Get(sessionID, owner); err != nil {
		return err
	}
	r.mu.Lock()
	owned := r.sessions[sessionID]
	delete(r.sessions, sessionID)
	r.mu.Unlock()
	if owned != nil {
		browserErr := owned.browser.Close()
		managerErr := r.manager.Close(ctx, sessionID, owner)
		if browserErr != nil || managerErr != nil {
			return errors.Join(browserErr, managerErr)
		}
		return nil
	}
	return r.manager.Close(ctx, sessionID, owner)
}
