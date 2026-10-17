package artemis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/profile"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

// ownedChromiumPage keeps references, observations and actions with one target.
type ownedChromiumPage struct {
	page     *bridge.Page
	observer *observe.LiveCollector
	actions  *actions.Runtime
	// recorder, lease, needsFreshObservation, visual and webmcp are guarded
	// by Session.chromiumMu: the active workflow recording, the exclusive
	// writer lease, the post-handoff re-observation requirement, the current
	// visual observation and the current WebMCP catalog.
	recorder              *observe.WorkflowCaptureRecorder
	lease                 *pageWriterLease
	needsFreshObservation bool
	visual                *visualBinding
	webmcp                *webMCPState
}

func (a *Agent) ownedChromium() (*bridge.ChromiumBrowser, error) {
	a.chromiumMu.Lock()
	defer a.chromiumMu.Unlock()
	a.mu.RLock()
	config, runCtx, state := a.config, a.runCtx, a.state
	a.mu.RUnlock()
	if state != AgentStateRunning || runCtx == nil {
		return nil, newTaskError(TaskErrorInvalidTransition, "chromium", fmt.Errorf("agent state %s", state))
	}
	if config.Chromium == nil {
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "chromium", fmt.Errorf("chromium control is not configured"))
	}
	if a.chromiumBrowser != nil {
		if !a.chromiumBrowser.Healthy() {
			return nil, newTaskError(TaskErrorBrowserCrash, "chromium", a.chromiumBrowser.Err())
		}
		return a.chromiumBrowser, nil
	}
	launch := *config.Chromium
	launch.NetworkPolicy = &config.PolicyConfig
	launch.DisableAdBlock = config.DisableAdBlock
	if config.AdBlock != nil {
		launch.AdBlock = config.AdBlock
	}
	launch.AllowPrivateNetworks = config.PolicyConfig.AllowPrivateNetworks
	launch.AllowedPorts = append([]int(nil), config.PolicyConfig.AllowedPorts...)
	if launch.StartupTimeout == 0 {
		launch.StartupTimeout = config.FetchTimeout
	}
	// Stop cancels operations first, then closes pages and CDP, then the process.
	// A separate owned lifetime avoids killing CDP before page teardown finishes.
	browserCtx, cancel := context.WithCancel(context.Background())
	browser, err := bridge.LaunchChromium(browserCtx, launch)
	if err != nil {
		cancel()
		a.chromiumStartErr = err
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "chromium_launch", fmt.Errorf("install/configure a compatible Chrome executable: %w", err))
	}
	if err := browser.SetMaxPages(config.MaxSessions * config.MaxTabs); err != nil {
		cancel()
		return nil, errors.Join(err, browser.Close())
	}
	a.chromiumBrowser = browser
	a.chromiumCancel = cancel
	a.chromiumStartErr = nil
	return browser, nil
}

func (s *Session) chromiumOperation(ctx context.Context) (context.Context, func(), *TaskError) {
	if s.owner == nil {
		return nil, nil, newTaskError(TaskErrorInvalidInput, "chromium", fmt.Errorf("session owner required"))
	}
	if ctx == nil {
		return nil, nil, newTaskError(TaskErrorInvalidInput, "chromium", fmt.Errorf("context required"))
	}
	sessionCtx, active := s.beginOperation()
	if !active {
		return nil, nil, newTaskError(TaskErrorStaleReference, "chromium", fmt.Errorf("session is closed"))
	}
	execCtx, cancel := context.WithTimeout(ctx, s.owner.Config().ScriptTimeout)
	stop := context.AfterFunc(sessionCtx, cancel)
	return execCtx, func() { stop(); cancel(); s.operations.Done() }, nil
}

// OpenChromiumPage creates a target inside this session's isolated CDP context.
func (s *Session) OpenChromiumPage(ctx context.Context, targetURL string) (string, observe.ObservationEvidence, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return "", observe.ObservationEvidence{}, taskErr
	}
	defer done()
	if err := s.AddTab(); err != nil {
		return "", observe.ObservationEvidence{}, err
	}
	page, err := s.newChromiumPage(execCtx, targetURL, artemisrouter.BrowserState{})
	if err != nil {
		err = errors.Join(err, s.releaseChromiumTab())
		return "", observe.ObservationEvidence{}, classifyTaskError("chromium_open", err)
	}
	evidence, err := page.observer.CaptureEvidence(execCtx)
	if err != nil {
		err = errors.Join(err, page.close(), s.releaseChromiumTab())
		return "", observe.ObservationEvidence{}, classifyTaskError("chromium_capture", err)
	}
	evidence, finding := s.chromiumAdBlockLayer2(execCtx, page, targetURL, evidence)
	s.chromiumMu.Lock()
	if s.chromiumPages == nil {
		s.chromiumPages = make(map[string]*ownedChromiumPage)
	}
	pageID := page.page.TargetID()
	s.chromiumPages[pageID] = page
	s.chromiumMu.Unlock()
	if err := page.page.AddCleanup(func() error {
		s.chromiumMu.Lock()
		owned := s.chromiumPages[pageID] == page
		if owned {
			delete(s.chromiumPages, pageID)
		}
		s.chromiumMu.Unlock()
		if owned {
			s.mu.Lock()
			if s.tabs > 0 {
				s.tabs--
			}
			delete(s.adBlockFindings, pageID)
			s.mu.Unlock()
		}
		return nil
	}); err != nil {
		return pageID, observe.ObservationEvidence{}, classifyTaskError("chromium_open", fmt.Errorf("page %s retired before ownership registration: %w", pageID, err))
	}
	s.recordAdBlockFinding(pageID, finding)
	bindPageRefs(pageID, &evidence)
	return pageID, evidence, nil
}

func (s *Session) newChromiumPage(ctx context.Context, targetURL string, state artemisrouter.BrowserState) (*ownedChromiumPage, error) {
	if err := s.owner.authorizeBrowserRequest(ctx, bridge.PolicyRequest{Action: "navigate", URL: targetURL, Method: "GET"}); err != nil {
		return nil, err
	}
	browser, err := s.owner.ownedChromium()
	if err != nil {
		return nil, err
	}
	s.chromiumMu.Lock()
	if s.chromiumContext == nil {
		s.chromiumContext, err = browser.NewContext(ctx)
		if err != nil {
			s.chromiumMu.Unlock()
			return nil, err
		}
	}
	owner := s.chromiumContext
	s.chromiumMu.Unlock()
	policy, _, err := stealth.PolicyFromEnv()
	if err != nil {
		return nil, err
	}
	scripts, err := profile.PrepareTargetScripts(ctx, browser, owner, profile.SessionID(s.id), targetURL, policy)
	if err != nil {
		return nil, err
	}
	scripts, err = storageTransferScripts(scripts, targetURL, state)
	if err != nil {
		return nil, err
	}
	page, err := owner.NewPageWithScripts(ctx, "about:blank", scripts)
	if err != nil {
		return nil, err
	}
	if err := transferCookies(ctx, page, targetURL, state); err != nil {
		return nil, errors.Join(err, page.Close())
	}
	observer, err := observe.NewLiveCollector(page, page, observe.DefaultLiveConfig())
	if err != nil {
		return nil, errors.Join(err, page.Close())
	}
	s.owner.mu.RLock()
	actionPolicy := s.owner.browserPolicy
	var inputMemory network.MemoryAdmission
	if runtime, ok := s.owner.runtime.(interface {
		InputMemoryAdmission() network.MemoryAdmission
	}); ok {
		inputMemory = runtime.InputMemoryAdmission()
	}
	s.owner.mu.RUnlock()
	runtime, err := actions.NewRuntimeWithConfig(page, observer.SnapshotCollector(), nil, actions.RuntimeConfig{
		PolicyEngine: actionPolicy, InputMemory: inputMemory, UserID: s.UserID(),
	})
	if err != nil {
		return nil, errors.Join(err, observer.Close(), page.Close())
	}
	owned := &ownedChromiumPage{page: page, observer: observer, actions: runtime}
	outcome := runtime.Execute(ctx, actions.Request{Kind: actions.KindNavigate, URL: targetURL})
	if !outcome.Success {
		return nil, errors.Join(fmt.Errorf("navigate: %s", outcome.Error), owned.close())
	}
	return owned, nil
}

func (s *Session) leaseChromiumPage(ctx context.Context, pageID string) (*ownedChromiumPage, *bridge.PageLease, *TaskError) {
	s.chromiumMu.Lock()
	page := s.chromiumPages[pageID]
	s.chromiumMu.Unlock()
	if page == nil {
		return nil, nil, newTaskError(TaskErrorPageNotFound, "chromium_page", fmt.Errorf("unknown page %q", pageID))
	}
	lease, err := page.page.AcquireExecution(ctx)
	if err != nil {
		return nil, nil, classifyTaskError("chromium_lease", err)
	}
	s.chromiumMu.Lock()
	current := s.chromiumPages[pageID] == page
	s.chromiumMu.Unlock()
	if !current {
		lease.Release()
		return nil, nil, newTaskError(TaskErrorPageNotFound, "chromium_page", fmt.Errorf("page %q closed while queued", pageID))
	}
	return page, lease, nil
}

// PageURL returns the current URL of a renderless or Chromium page; content
// trust labels use it as the origin of page-derived output.
func (s *Session) PageURL(ctx context.Context, pageID string) (string, *TaskError) {
	if page := s.Page(pageID); page != nil {
		return page.URL(), nil
	}
	s.chromiumMu.Lock()
	page := s.chromiumPages[pageID]
	s.chromiumMu.Unlock()
	if page == nil {
		return "", newTaskError(TaskErrorPageNotFound, "page_url", fmt.Errorf("unknown page %q", pageID))
	}
	pageURL, err := page.page.CurrentURL(ctx)
	if err != nil {
		return "", classifyTaskError("page_url", err)
	}
	return pageURL, nil
}

// ObserveChromiumPage uses the same reference collector across invocations.
func (s *Session) ObserveChromiumPage(ctx context.Context, pageID string, mode bridgeobserve.Mode, subtreeRef string) (observe.ObservationEvidence, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return observe.ObservationEvidence{}, taskErr
	}
	defer done()
	page, release, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return observe.ObservationEvidence{}, taskErr
	}
	defer release.Release()
	execCtx = release.Context()
	if subtreeRef != "" {
		var err error
		subtreeRef, err = pageReference(pageID, subtreeRef)
		if err != nil {
			return observe.ObservationEvidence{}, classifyTaskError("chromium_subtree", err)
		}
	}
	if taskErr := s.authorizeChromiumOperation(execCtx, page, "observe", "", subtreeRef); taskErr != nil {
		return observe.ObservationEvidence{}, taskErr
	}
	evidence, err := page.observer.Capture(execCtx, mode, subtreeRef)
	if err != nil {
		return observe.ObservationEvidence{}, classifyTaskError("chromium_observe", err)
	}
	s.markObserved(page)
	bindPageRefs(pageID, &evidence)
	return evidence, nil
}

// ActChromiumPage requires an explicit page and document epoch for ref actions.
func (s *Session) ActChromiumPage(ctx context.Context, pageID string, epoch uint64, request actions.Request) (actions.Outcome, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return actions.Outcome{}, taskErr
	}
	defer done()
	var page *ownedChromiumPage
	var lease *bridge.PageLease
	if request.Kind == actions.KindDialog {
		if err := actions.ValidateRequest(request); err != nil {
			return actions.Outcome{}, newTaskError(TaskErrorInvalidInput, "chromium_act", err)
		}
		s.chromiumMu.Lock()
		page = s.chromiumPages[pageID]
		s.chromiumMu.Unlock()
		if page == nil {
			return actions.Outcome{}, newTaskError(TaskErrorPageNotFound, "chromium_page", fmt.Errorf("unknown page %q", pageID))
		}
	} else {
		page, lease, taskErr = s.leaseChromiumPage(execCtx, pageID)
		if taskErr != nil {
			return actions.Outcome{}, taskErr
		}
		defer lease.Release()
		execCtx = lease.Context()
	}
	if request.Pointer != nil {
		return actions.Outcome{}, newTaskError(TaskErrorInvalidInput, "chromium_act", errors.New("pointer targets require a bound visual observation; use ActVisual"))
	}
	if taskErr := s.authorizeWrite(ctx, page, request.Ref != "" || request.TargetRef != ""); taskErr != nil {
		return actions.Outcome{}, taskErr
	}
	recorder := s.pageRecorder(page)
	var before bridgeobserve.Snapshot
	if request.Ref != "" || request.TargetRef != "" || recorder != nil {
		snapshot, err := page.observer.CaptureEvidence(execCtx)
		if err != nil {
			return actions.Outcome{}, classifyTaskError("chromium_observe", err)
		}
		if (request.Ref != "" || request.TargetRef != "") && (epoch == 0 || snapshot.Snapshot.Epoch != epoch) {
			return actions.Outcome{}, newTaskError(TaskErrorStaleReference, "chromium_act", fmt.Errorf("ref requires current document epoch %d", snapshot.Snapshot.Epoch))
		}
		before = snapshot.Snapshot
	}
	started := time.Now()
	for _, ref := range []*string{&request.Ref, &request.TargetRef} {
		if *ref == "" {
			continue
		}
		value, err := pageReference(pageID, *ref)
		if err != nil {
			return actions.Outcome{}, classifyTaskError("chromium_ref", err)
		}
		*ref = value
	}
	if request.TargetID != "" && request.TargetID != page.page.TargetID() {
		return actions.Outcome{}, newTaskError(TaskErrorPolicyDenied, "chromium_act", fmt.Errorf("target is outside the explicit page"))
	}
	switch request.Kind {
	case actions.KindTabOpen, actions.KindTabSwitch, actions.KindTabClose, actions.KindTabList:
		return actions.Outcome{}, newTaskError(TaskErrorInvalidInput, "chromium_act", fmt.Errorf("use session-owned page operations for tabs"))
	}
	var outcome actions.Outcome
	if lease != nil {
		outcome = page.actions.ExecuteLeased(request, lease)
	} else {
		outcome = page.actions.Execute(execCtx, request)
	}
	if recorder != nil {
		after, err := page.observer.CaptureEvidence(execCtx)
		if err == nil {
			err = recordAction(execCtx, recorder, before, after.Snapshot, request, outcome, started)
		}
		if err != nil {
			outcome.Error = strings.TrimPrefix(outcome.Error+"; workflow recording failed: "+err.Error(), "; ")
		}
	}
	if outcome.Evidence.Ref != "" {
		outcome.Evidence.Ref = pageID + ":" + outcome.Evidence.Ref
	}
	return outcome, nil
}

func bindPageRefs(pageID string, evidence *observe.ObservationEvidence) {
	for i := range evidence.Snapshot.Nodes {
		if evidence.Snapshot.Nodes[i].Ref != "" {
			evidence.Snapshot.Nodes[i].Ref = pageID + ":" + evidence.Snapshot.Nodes[i].Ref
		}
	}
}

func pageReference(pageID, ref string) (string, error) {
	ref = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(ref), "@"), "ref=")
	if !strings.HasPrefix(ref, pageID+":") {
		return "", newTaskError(TaskErrorStaleReference, "chromium_ref", fmt.Errorf("ref does not belong to explicit page"))
	}
	return strings.TrimPrefix(ref, pageID+":"), nil
}

// CloseChromiumPage serializes teardown with actions against that page.
func (s *Session) CloseChromiumPage(ctx context.Context, pageID string) *TaskError {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return taskErr
	}
	defer done()
	page, release, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return taskErr
	}
	defer release.Release()
	execCtx = release.Context()
	if taskErr := s.authorizeChromiumOperation(execCtx, page, "tab_close", ""); taskErr != nil {
		return taskErr
	}
	err := page.page.CloseContext(execCtx)
	if err != nil {
		return classifyTaskError("chromium_close", err)
	}
	return nil
}

func (p *ownedChromiumPage) close() error {
	return p.page.Close()
}

func (s *Session) releaseChromiumTab() error {
	if err := s.RemoveTab(); err != nil {
		return err
	}
	return nil
}

func (s *Session) closeChromiumPages() error {
	s.chromiumMu.Lock()
	pages := make([]*ownedChromiumPage, 0, len(s.chromiumPages))
	for id, page := range s.chromiumPages {
		pages = append(pages, page)
		delete(s.chromiumPages, id)
	}
	owner := s.chromiumContext
	s.chromiumContext = nil
	s.chromiumMu.Unlock()
	var err error
	for _, page := range pages {
		err = errors.Join(err, page.close())
	}
	if owner != nil {
		err = errors.Join(err, owner.Close())
	}
	return err
}
