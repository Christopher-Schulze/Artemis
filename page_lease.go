package artemis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// page_lease.go owns exclusive write authority over a session Chromium page
// (spec L4580-L4604 browser session authority) and the authenticated local
// handoff of that page to a human, for MFA or dialogs. A writer lease is
// exclusive and expiring; observation stays available to readers. While a
// lease is held, actions without its token are refused. Ending a handoff
// requires fresh observation before the next reference action.

const (
	maxWriterLeaseTTL   = 30 * time.Minute
	handoffOpenTokenTTL = time.Minute
)

// WriterLease is the exclusive write authority over one page.
type WriterLease struct {
	Token     string    `json:"token"`
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// HandoffTicket hands a page to a human: LeaseToken is the human's writer
// lease, OpenToken a one-time token that reveals the inspector URL.
type HandoffTicket struct {
	LeaseToken string    `json:"leaseToken"`
	OpenToken  string    `json:"openToken"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type pageWriterLease struct {
	token   string
	holder  string
	expires time.Time
}

type handoffOpen struct {
	pageID  string
	expires time.Time
}

type writerLeaseKey struct{}

// WithWriterLease authorizes actions made with ctx under a writer lease.
func WithWriterLease(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, writerLeaseKey{}, token)
}

func writerLeaseToken(ctx context.Context) string {
	token, _ := ctx.Value(writerLeaseKey{}).(string)
	return token
}

func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// AcquireWriterLease grants holder exclusive write authority over a page for
// ttl (at most 30 minutes). A live lease of another holder is refused.
func (s *Session) AcquireWriterLease(pageID, holder string, ttl time.Duration) (WriterLease, *TaskError) {
	if strings.TrimSpace(holder) == "" || ttl <= 0 || ttl > maxWriterLeaseTTL {
		return WriterLease{}, newTaskError(TaskErrorInvalidInput, "writer_lease", errors.New("holder and a ttl up to 30m required"))
	}
	token, err := randomToken()
	if err != nil {
		return WriterLease{}, classifyTaskError("writer_lease", err)
	}
	now := time.Now()
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	page := s.chromiumPages[pageID]
	if page == nil {
		return WriterLease{}, newTaskError(TaskErrorPageNotFound, "writer_lease", fmt.Errorf("unknown page %q", pageID))
	}
	if page.lease != nil && now.Before(page.lease.expires) {
		return WriterLease{}, newTaskError(TaskErrorPolicyDenied, "writer_lease", fmt.Errorf("page is held by %s until %s", page.lease.holder, page.lease.expires.Format(time.RFC3339)))
	}
	page.lease = &pageWriterLease{token: token, holder: holder, expires: now.Add(ttl)}
	return WriterLease{Token: token, Holder: holder, ExpiresAt: page.lease.expires}, nil
}

// ReleaseWriterLease ends a lease; only its token may release it.
func (s *Session) ReleaseWriterLease(pageID, token string) *TaskError {
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	page := s.chromiumPages[pageID]
	if page == nil {
		return newTaskError(TaskErrorPageNotFound, "writer_lease", fmt.Errorf("unknown page %q", pageID))
	}
	if page.lease == nil || page.lease.token != token {
		return newTaskError(TaskErrorPolicyDenied, "writer_lease", errors.New("lease token does not hold this page"))
	}
	page.lease = nil
	return nil
}

// authorizeWrite refuses a write to a page leased by another holder and a
// reference action that has not re-observed the page after a handoff.
func (s *Session) authorizeWrite(ctx context.Context, page *ownedChromiumPage, usesRef bool) *TaskError {
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	if lease := page.lease; lease != nil && time.Now().Before(lease.expires) && writerLeaseToken(ctx) != lease.token {
		return newTaskError(TaskErrorPolicyDenied, "writer_lease", fmt.Errorf("page is held by %s", lease.holder))
	}
	if usesRef && page.needsFreshObservation {
		return newTaskError(TaskErrorStaleReference, "handoff", errors.New("observe the page again after the handoff before acting on references"))
	}
	return nil
}

func (s *Session) markObserved(page *ownedChromiumPage) {
	s.chromiumMu.Lock()
	page.needsFreshObservation = false
	s.chromiumMu.Unlock()
}

// BeginHandoff gives a human exclusive write authority over the page for ttl
// and issues a one-time token for the page's inspector.
func (s *Session) BeginHandoff(pageID string, ttl time.Duration) (HandoffTicket, *TaskError) {
	lease, taskErr := s.AcquireWriterLease(pageID, "human", ttl)
	if taskErr != nil {
		return HandoffTicket{}, taskErr
	}
	open, err := randomToken()
	if err != nil {
		if releaseErr := s.ReleaseWriterLease(pageID, lease.Token); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return HandoffTicket{}, classifyTaskError("handoff", err)
	}
	s.mu.Lock()
	if s.handoffs == nil {
		s.handoffs = map[string]handoffOpen{}
	}
	s.handoffs[open] = handoffOpen{pageID: pageID, expires: time.Now().Add(handoffOpenTokenTTL)}
	s.mu.Unlock()
	return HandoffTicket{LeaseToken: lease.Token, OpenToken: open, ExpiresAt: lease.ExpiresAt}, nil
}

// OpenHandoff consumes a one-time open token and returns the DevTools
// inspector URL of the handed-off page on the local CDP endpoint.
func (s *Session) OpenHandoff(openToken string) (string, *TaskError) {
	s.mu.Lock()
	entry, ok := s.handoffs[openToken]
	delete(s.handoffs, openToken)
	s.mu.Unlock()
	if !ok || time.Now().After(entry.expires) {
		return "", newTaskError(TaskErrorPolicyDenied, "handoff", errors.New("handoff token is unknown, used or expired"))
	}
	s.chromiumMu.Lock()
	page := s.chromiumPages[entry.pageID]
	s.chromiumMu.Unlock()
	if page == nil {
		return "", newTaskError(TaskErrorPageNotFound, "handoff", fmt.Errorf("unknown page %q", entry.pageID))
	}
	browser, err := s.owner.ownedChromium()
	if err != nil {
		return "", classifyTaskError("handoff", err)
	}
	endpoint, err := url.Parse(browser.Endpoint())
	if err != nil || endpoint.Host == "" {
		return "", newTaskError(TaskErrorCapabilityUnavailable, "handoff", errors.New("browser has no local CDP endpoint"))
	}
	socket := endpoint.Host + "/devtools/page/" + page.page.TargetID()
	return "http://" + endpoint.Host + "/devtools/inspector.html?ws=" + url.QueryEscape(socket), nil
}

// EndHandoff returns the page to automation; the next reference action
// must follow a fresh observation.
func (s *Session) EndHandoff(pageID, leaseToken string) *TaskError {
	if taskErr := s.ReleaseWriterLease(pageID, leaseToken); taskErr != nil {
		return taskErr
	}
	s.chromiumMu.Lock()
	if page := s.chromiumPages[pageID]; page != nil {
		page.needsFreshObservation = true
	}
	s.chromiumMu.Unlock()
	return nil
}
