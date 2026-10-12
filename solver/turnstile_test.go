package solver

import (
	"context"
	"testing"
	"time"
)

func TestTurnstileSolverWaitAndDetect(t *testing.T) {
	callCount := 0
	pageProvider := func() (PageSignals, error) {
		callCount++
		if callCount < 3 {
			return PageSignals{Title: "Just a moment...", HTML: "<html>cloudflare</html>"}, nil
		}
		return PageSignals{Title: "Welcome", HTML: "<html>content</html>"}, nil
	}

	solver := NewTurnstileSolver(10 * time.Second)
	challenge := &ChallengeInfo{Type: TypeCloudflare, Confidence: 0.95}
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Solved {
		t.Fatalf("expected solved, got error: %s", result.Error)
	}
	if result.Method != MethodWaitAndDetect {
		t.Fatalf("expected wait_and_detect, got %s", result.Method)
	}
	if result.TimeTaken <= 0 {
		t.Fatal("expected positive time taken")
	}
}

func TestTurnstileSolverTimeout(t *testing.T) {
	pageProvider := func() (PageSignals, error) {
		return PageSignals{Title: "Just a moment...", HTML: "<html>cloudflare</html>"}, nil
	}

	solver := NewTurnstileSolver(2 * time.Second)
	challenge := &ChallengeInfo{Type: TypeCloudflare, Confidence: 0.95}
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Solved {
		t.Fatal("expected not solved on timeout")
	}
}

func TestTurnstileSolverNoChallenge(t *testing.T) {
	pageProvider := func() (PageSignals, error) {
		return PageSignals{Title: "Welcome", HTML: "<html>content</html>"}, nil
	}

	solver := NewTurnstileSolver(5 * time.Second)
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Solved {
		t.Fatal("expected not solved when no challenge")
	}
	if result.Method != MethodNone {
		t.Fatalf("expected MethodNone, got %s", result.Method)
	}
}

func TestTurnstileSolverWrongChallengeType(t *testing.T) {
	pageProvider := func() (PageSignals, error) {
		return PageSignals{Title: "Captcha", HTML: "<html>recaptcha</html>"}, nil
	}

	solver := NewTurnstileSolver(5 * time.Second)
	challenge := &ChallengeInfo{Type: TypeRecaptcha, Confidence: 0.9}
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Solved {
		t.Fatal("expected not solved for non-cloudflare challenge")
	}
}

func TestTurnstileSolverNilPageProvider(t *testing.T) {
	solver := NewTurnstileSolver(5 * time.Second)
	challenge := &ChallengeInfo{Type: TypeCloudflare}
	_, err := solver.SolveTurnstile(context.Background(), nil, challenge)
	if err == nil {
		t.Fatal("expected error for nil page provider")
	}
}

func TestTurnstileSolverTokenExtraction(t *testing.T) {
	pageProvider := func() (PageSignals, error) {
		return PageSignals{
			Title: "Just a moment...",
			HTML:  `<html><script>var cf_clearance = "abc123";</script></html>`,
		}, nil
	}

	solver := NewTurnstileSolver(2 * time.Second)
	challenge := &ChallengeInfo{Type: TypeCloudflare, Confidence: 0.95}
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Solved {
		t.Fatalf("expected solved via token extraction, got: %s", result.Error)
	}
	if result.Method != MethodTokenExtract {
		t.Fatalf("expected token_extract, got %s", result.Method)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestTurnstileSolverTurnstileToken(t *testing.T) {
	pageProvider := func() (PageSignals, error) {
		return PageSignals{
			Title: "Just a moment...",
			HTML:  `<html><input name="cf-turnstile-response" value="token123"></html>`,
		}, nil
	}

	solver := NewTurnstileSolver(2 * time.Second)
	challenge := &ChallengeInfo{Type: TypeCloudflare, Confidence: 0.95}
	result, err := solver.SolveTurnstile(context.Background(), pageProvider, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Solved {
		t.Fatalf("expected solved via turnstile token, got: %s", result.Error)
	}
	if result.Token != "turnstile_token" {
		t.Fatalf("expected turnstile_token, got %s", result.Token)
	}
}

func TestTurnstileSolverDetectChallenge(t *testing.T) {
	solver := NewTurnstileSolver(5 * time.Second)
	page := PageSignals{
		Title: "Just a moment...",
		HTML:  `<iframe src="https://challenges.cloudflare.com/turnstile"></iframe>`,
	}
	info, err := solver.DetectChallenge(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.Type != TypeCloudflare {
		t.Fatalf("expected cloudflare detection, got %+v", info)
	}
}

func TestNewTurnstileSolverDefaultTimeout(t *testing.T) {
	solver := NewTurnstileSolver(0)
	if solver.timeout != 30*time.Second {
		t.Fatalf("expected 30s default timeout, got %v", solver.timeout)
	}
}

func TestSolveResultString(t *testing.T) {
	result := &SolveResult{Solved: true, Method: MethodWaitAndDetect}
	if !result.Solved {
		t.Fatal("expected solved")
	}
	if result.Method != MethodWaitAndDetect {
		t.Fatalf("expected wait_and_detect, got %s", result.Method)
	}
}

func TestSolveMethodValues(t *testing.T) {
	if MethodWaitAndDetect != "wait_and_detect" {
		t.Fatal("expected wait_and_detect")
	}
	if MethodTokenExtract != "token_extract" {
		t.Fatal("expected token_extract")
	}
	if MethodInteraction != "interaction" {
		t.Fatal("expected interaction")
	}
	if MethodNone != "none" {
		t.Fatal("expected none")
	}
}
