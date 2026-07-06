package solver

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SolveMethod describes the strategy used to solve a Turnstile challenge.
type SolveMethod string

const (
	MethodWaitAndDetect SolveMethod = "wait_and_detect" // Wait for auto-solve (non-interactive)
	MethodTokenExtract  SolveMethod = "token_extract"   // Extract cf_clearance cookie or turnstile token
	MethodInteraction   SolveMethod = "interaction"     // Click the challenge checkbox
	MethodNone          SolveMethod = "none"            // No solving attempted
)

// SolveResult is the outcome of a Turnstile solve attempt.
type SolveResult struct {
	Solved    bool
	Method    SolveMethod
	TimeTaken time.Duration
	Token     string // cf_clearance cookie or turnstile token if extracted
	Error     string // error description if not solved
}

// TurnstileSolver attempts to solve Cloudflare Turnstile challenges.
// Strategies (per Scrapling _stealth.py:106-560):
// 1. wait-and-detect: Turnstile auto-solves in 5-15s for trusted browsers (non-interactive).
// 2. token-extraction: Extract cf_clearance cookie or turnstile token from page.
// 3. interaction-based: Click the challenge checkbox if visible.
type TurnstileSolver struct {
	detector *ChallengeDetector
	timeout  time.Duration
}

// NewTurnstileSolver creates a TurnstileSolver with the given timeout.
// Default timeout is 30s.
func NewTurnstileSolver(timeout time.Duration) *TurnstileSolver {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &TurnstileSolver{
		detector: NewChallengeDetector(),
		timeout:  timeout,
	}
}

// SolveTurnstile attempts to solve a Cloudflare Turnstile challenge.
// pageProvider supplies current page HTML/title for detection and monitoring.
// Returns SolveResult with solved status, method used, and timing.
func (s *TurnstileSolver) SolveTurnstile(ctx context.Context, pageProvider func() (PageSignals, error), challenge *ChallengeInfo) (*SolveResult, error) {
	if s == nil {
		return nil, fmt.Errorf("turnstile: solver is nil")
	}
	if pageProvider == nil {
		return nil, fmt.Errorf("turnstile: page provider required")
	}
	if challenge == nil || challenge.Type != TypeCloudflare {
		return &SolveResult{Solved: false, Method: MethodNone, Error: "no cloudflare challenge detected"}, nil
	}

	start := time.Now()
	deadline := start.Add(s.timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	// Strategy 1: wait-and-detect for non-interactive Turnstile
	// Turnstile auto-solves in 5-15s for trusted browsers
	result, err := s.waitAndDetect(ctx, pageProvider, start)
	if err != nil {
		return nil, err
	}
	if result.Solved {
		return result, nil
	}

	// Strategy 2: token extraction (cf_clearance cookie or turnstile token)
	result = s.tryTokenExtraction(ctx, pageProvider, start)
	if result.Solved {
		return result, nil
	}

	// Strategy 3: interaction-based (would require CDP mouse events in real impl)
	// This is a fallback for interactive challenges
	return &SolveResult{
		Solved:    false,
		Method:    MethodInteraction,
		TimeTaken: time.Since(start),
		Error:     "interaction-based solving requires CDP mouse events",
	}, nil
}

// waitAndDetect waits for the Turnstile challenge to auto-solve.
// For non-interactive challenges, the page transitions from "Just a moment..."
// to the actual content within 5-15s for trusted browsers.
func (s *TurnstileSolver) waitAndDetect(ctx context.Context, pageProvider func() (PageSignals, error), start time.Time) (*SolveResult, error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &SolveResult{
				Solved:    false,
				Method:    MethodWaitAndDetect,
				TimeTaken: time.Since(start),
				Error:     "timeout waiting for auto-solve",
			}, nil
		case <-ticker.C:
			page, err := pageProvider()
			if err != nil {
				return nil, fmt.Errorf("turnstile: page provider error: %w", err)
			}
			// Check if "Just a moment..." title is gone
			if !strings.Contains(strings.ToLower(page.Title), "just a moment") {
				// Verify no more cloudflare challenge
				info, _ := s.detector.Detect(ctx, page)
				if info == nil || info.Type != TypeCloudflare {
					return &SolveResult{
						Solved:    true,
						Method:    MethodWaitAndDetect,
						TimeTaken: time.Since(start),
					}, nil
				}
			}
		}
	}
}

// tryTokenExtraction attempts to extract a cf_clearance cookie or turnstile
// token from the page signals. This works when the challenge has been solved
// but the page hasn't fully redirected yet.
func (s *TurnstileSolver) tryTokenExtraction(ctx context.Context, pageProvider func() (PageSignals, error), start time.Time) *SolveResult {
	page, err := pageProvider()
	if err != nil {
		return &SolveResult{
			Solved:    false,
			Method:    MethodTokenExtract,
			TimeTaken: time.Since(start),
			Error:     fmt.Sprintf("page provider error: %v", err),
		}
	}

	// Check for cf_clearance cookie in page content (simplified check)
	html := strings.ToLower(page.HTML)
	if strings.Contains(html, "cf_clearance") {
		return &SolveResult{
			Solved:    true,
			Method:    MethodTokenExtract,
			TimeTaken: time.Since(start),
			Token:     "cf_clearance",
		}
	}

	// Check for turnstile token in page content
	if strings.Contains(html, "cf-turnstile-response") || strings.Contains(html, "turnstile_token") {
		return &SolveResult{
			Solved:    true,
			Method:    MethodTokenExtract,
			TimeTaken: time.Since(start),
			Token:     "turnstile_token",
		}
	}

	return &SolveResult{
		Solved:    false,
		Method:    MethodTokenExtract,
		TimeTaken: time.Since(start),
		Error:     "no token found in page",
	}
}

// DetectChallenge is a convenience method that uses the internal detector
// to check if a page has a Cloudflare challenge.
func (s *TurnstileSolver) DetectChallenge(ctx context.Context, page PageSignals) (*ChallengeInfo, error) {
	return s.detector.Detect(ctx, page)
}
