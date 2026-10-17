package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxAuthChallengeEventBytes = 64 << 10

type fetchAuthChallengeResponse struct {
	Response string `json:"response"`
}

type fetchContinueAuthParams struct {
	RequestID             string                     `json:"requestId"`
	AuthChallengeResponse fetchAuthChallengeResponse `json:"authChallengeResponse"`
}

func decodeAuthChallengeJob(event CDPEvent) (fetchRequestJob, error) {
	if len(event.Params) > maxAuthChallengeEventBytes || !utf8.Valid(event.Params) {
		return fetchRequestJob{}, fmt.Errorf("auth challenge event exceeds limit or is not UTF-8")
	}
	var payload struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(event.Params, &payload); err != nil {
		return fetchRequestJob{}, fmt.Errorf("invalid auth challenge event")
	}
	if payload.RequestID == "" || len(payload.RequestID) > 256 || strings.ContainsAny(payload.RequestID, "\x00\r\n") {
		return fetchRequestJob{}, fmt.Errorf("auth challenge requires a bounded request identity")
	}
	return fetchRequestJob{sessionID: event.SessionID, payload: fetchRequestPaused{RequestID: payload.RequestID}, authRequired: true}, nil
}

func (p *Page) enqueueAuthChallenge(event CDPEvent, jobs chan<- fetchRequestJob) bool {
	job, err := decodeAuthChallengeJob(event)
	if err != nil {
		p.failNetworkEnforcement(err)
		return false
	}
	select {
	case jobs <- job:
		return true
	default:
		p.failNetworkEnforcement(fmt.Errorf("auth challenge queue exhausted"))
		return false
	}
}

// An unhandled challenge never borrows ambient credentials or prompts from a
// headless worker. The original unauthorized response remains browser-owned.
func (p *Page) cancelAuthChallenge(ctx context.Context, sessionID, requestID string) error {
	if ctx == nil || ctx.Err() != nil {
		if ctx != nil {
			return ctx.Err()
		}
		return fmt.Errorf("auth challenge requires a live context")
	}
	if p == nil || p.owner == nil || p.owner.browser == nil {
		return fmt.Errorf("auth challenge requires an owning page")
	}
	p.owner.mu.Lock()
	closed := p.owner.closed
	p.owner.mu.Unlock()
	if closed {
		return &TargetError{TargetID: p.targetID, State: TargetStateClosed, Message: "owning context closed"}
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	if !p.ownsPolicySession(sessionID) {
		return fmt.Errorf("auth challenge session is not owned by the page")
	}
	if requestID == "" || len(requestID) > 256 {
		return fmt.Errorf("auth challenge request identity is invalid")
	}
	params := fetchContinueAuthParams{RequestID: requestID, AuthChallengeResponse: fetchAuthChallengeResponse{Response: "CancelAuth"}}
	return p.owner.browser.transport.CallSession(ctx, sessionID, "Fetch.continueWithAuth", params, nil)
}
