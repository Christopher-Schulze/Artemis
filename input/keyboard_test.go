package input

import (
	"testing"
	"time"
)

func TestTypingRhythmGaussian(t *testing.T) {
	cfg := DefaultTypingConfig()
	delays, keys := TypingRhythm("Hello!", cfg, nil)
	if len(keys) < len("Hello!") {
		t.Fatalf("expected at least %d key events, got %d", len("Hello!"), len(keys))
	}
	total := TotalTypingDuration(delays)
	if total < 200*time.Millisecond {
		t.Fatalf("paranoid rhythm too fast: %v", total)
	}
	if total > 5*time.Second {
		t.Fatalf("paranoid rhythm implausible: %v", total)
	}
	base := baseDelayMs('e')
	j := GaussianJitter(base, cfg.SigmaPct, nil)
	if j < 40 || j > 500 {
		t.Fatalf("jitter out of range: %d", j)
	}
}
