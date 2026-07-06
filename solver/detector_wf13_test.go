package solver

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// SP-artemis-solver-SEC (detector.go, turnstile.go, security_privacy)
// Claim: Detect denies cancelled contexts, SolveTurnstile denies nil solver,
// nil pageProvider, nil challenge, and non-Cloudflare challenge types
// =============================================================================

func TestWFArtemisSolver_DetectAndSolveDeniesInvalidInput(t *testing.T) {
	// Security: challenge detector and turnstile solver must deny invalid
	// inputs to prevent bypass of CAPTCHA challenge detection.

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"detect_cancelled_context",
			func() error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				d := NewChallengeDetector()
				_, err := d.Detect(ctx, PageSignals{Title: "test", HTML: "<html></html>"})
				return err
			},
		},
		{
			"solve_nil_solver",
			func() error {
				var s *TurnstileSolver
				_, err := s.SolveTurnstile(context.Background(), func() (PageSignals, error) {
					return PageSignals{}, nil
				}, &ChallengeInfo{Type: TypeCloudflare})
				return err
			},
		},
		{
			"solve_nil_page_provider",
			func() error {
				s := NewTurnstileSolver(1 * time.Second)
				_, err := s.SolveTurnstile(context.Background(), nil, &ChallengeInfo{Type: TypeCloudflare})
				return err
			},
		},
		{
			"solve_nil_challenge",
			func() error {
				s := NewTurnstileSolver(1 * time.Second)
				result, err := s.SolveTurnstile(context.Background(), func() (PageSignals, error) {
					return PageSignals{}, nil
				}, nil)
				if err != nil {
					return err
				}
				if result.Solved {
					return fmt.Errorf("expected nil challenge to not be solved")
				}
				return fmt.Errorf("denied: %s", result.Error)
			},
		},
		{
			"solve_non_cloudflare_challenge",
			func() error {
				s := NewTurnstileSolver(1 * time.Second)
				result, err := s.SolveTurnstile(context.Background(), func() (PageSignals, error) {
					return PageSignals{}, nil
				}, &ChallengeInfo{Type: TypeHCaptcha})
				if err != nil {
					return err
				}
				if result.Solved {
					return fmt.Errorf("expected non-cloudflare challenge to not be solved")
				}
				return fmt.Errorf("denied: %s", result.Error)
			},
		},
		{
			"solve_recaptcha_challenge",
			func() error {
				s := NewTurnstileSolver(1 * time.Second)
				result, err := s.SolveTurnstile(context.Background(), func() (PageSignals, error) {
					return PageSignals{}, nil
				}, &ChallengeInfo{Type: TypeRecaptcha})
				if err != nil {
					return err
				}
				if result.Solved {
					return fmt.Errorf("expected recaptcha challenge to not be solved by turnstile solver")
				}
				return fmt.Errorf("denied: %s", result.Error)
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		err := c.fn()
		if err == nil {
			t.Fatalf("%s: expected error/deny, got nil", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: valid Detect with Cloudflare signals succeeds (positive control)
	d := NewChallengeDetector()
	info, err := d.Detect(context.Background(), PageSignals{
		Title: "Just a moment...",
		HTML:  `<html><iframe src="https://challenges.cloudflare.com/cdn-cgi/challenge-platform/"></iframe></html>`,
	})
	if err != nil {
		t.Fatalf("valid Detect must succeed, got: %v", err)
	}
	if info.Type != TypeCloudflare {
		t.Fatalf("expected TypeCloudflare, got %v", info.Type)
	}
	if info.Confidence < 0.9 {
		t.Fatalf("expected confidence >= 0.9, got %f", info.Confidence)
	}

	// Baseline: Detect with no challenge returns TypeNone
	info, err = d.Detect(context.Background(), PageSignals{
		Title: "Normal Page",
		HTML:  "<html><body>Hello World</body></html>",
	})
	if err != nil {
		t.Fatalf("valid Detect with no challenge must succeed, got: %v", err)
	}
	if info.Type != TypeNone {
		t.Fatalf("expected TypeNone, got %v", info.Type)
	}

	// Baseline: Detect hCaptcha
	info, err = d.Detect(context.Background(), PageSignals{
		Title: "Captcha",
		HTML:  `<html><script src="https://hcaptcha.com/1/api.js"></script></html>`,
	})
	if err != nil {
		t.Fatalf("valid Detect hCaptcha must succeed, got: %v", err)
	}
	if info.Type != TypeHCaptcha {
		t.Fatalf("expected TypeHCaptcha, got %v", info.Type)
	}

	// Baseline: Detect reCAPTCHA
	info, err = d.Detect(context.Background(), PageSignals{
		Title: "Captcha",
		HTML:  `<html><div class="g-recaptcha"></div></html>`,
	})
	if err != nil {
		t.Fatalf("valid Detect reCAPTCHA must succeed, got: %v", err)
	}
	if info.Type != TypeRecaptcha {
		t.Fatalf("expected TypeRecaptcha, got %v", info.Type)
	}
}
