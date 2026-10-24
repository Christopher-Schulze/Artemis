package solver

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestArtemisSolver_DetectAndSolveDeniesInvalidInput verifies:
// Six actual local cancelled-context, missing-dependency and unsupported-challenge rejections. Each invalid-challenge case must report an unsolved result with a reason; erroneous Solved=true or absent result/reason now fails its named subtest rather than being converted into a fabricated denial. Positive detector fixtures do not prove a successful real-browser CAPTCHA solve.

func TestArtemisSolver_DetectAndSolveDeniesInvalidInput(t *testing.T) {

	// Measure only the actual local rejection cases described above.

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
				if result == nil || result.Solved || result.Error == "" {
					return nil
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
				if result == nil || result.Solved || result.Error == "" {
					return nil
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
				if result == nil || result.Solved || result.Error == "" {
					return nil
				}
				return fmt.Errorf("denied: %s", result.Error)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.fn()
			if err == nil {
				t.Fatalf("%s: expected error/deny, got nil", c.name)
			}
		})
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
