package input

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"
	"unicode"
)

// TypingConfig controls paranoid typing rhythm (Patch 28).
type TypingConfig struct {
	TypoRate float64
	SigmaPct float64
}

// DefaultTypingConfig returns spec defaults (2% typo, 15% jitter).
func DefaultTypingConfig() TypingConfig {
	return TypingConfig{TypoRate: 0.02, SigmaPct: 0.15}
}

var frequentLetters = map[rune]bool{
	'e': true, 't': true, 'a': true, 'o': true, 'i': true, 'n': true,
}

var rareLetters = map[rune]bool{
	'q': true, 'z': true, 'x': true, 'j': true,
}

// baseDelayMs returns the base inter-key delay for rune r.
func baseDelayMs(r rune) int {
	lower := unicode.ToLower(r)
	switch {
	case r == ' ':
		return 150
	case strings.ContainsRune(".,!?;:", r):
		return 200
	case rareLetters[lower]:
		return 200
	case frequentLetters[lower]:
		return 100
	default:
		return 130
	}
}

// TypingDelays returns per-rune delays in milliseconds for text.
func TypingDelays(text string, cfg TypingConfig, rng *rand.Rand) []time.Duration {
	if rng == nil {
		rng = rand.New(rand.NewPCG(7, 8))
	}
	if cfg.SigmaPct <= 0 {
		cfg.SigmaPct = 0.15
	}
	out := make([]time.Duration, 0, len(text))
	afterShift := false
	for _, r := range text {
		base := baseDelayMs(r)
		if afterShift {
			base += 50
			afterShift = false
		}
		if r >= 'A' && r <= 'Z' {
			afterShift = true
		}
		if strings.ContainsRune(".,!?;:", r) {
			base += 100
		}
		jitter := int(float64(base) * cfg.SigmaPct * rng.NormFloat64())
		ms := base + jitter
		if ms < 40 {
			ms = 40
		}
		out = append(out, time.Duration(ms)*time.Millisecond)
	}
	return out
}

// TypingRhythm simulates paranoid typing including optional typos.
func TypingRhythm(text string, cfg TypingConfig, rng *rand.Rand) (delays []time.Duration, keys []rune) {
	if rng == nil {
		rng = rand.New(rand.NewPCG(9, 10))
	}
	delays = TypingDelays(text, cfg, rng)
	keys = []rune(text)
	if cfg.TypoRate <= 0 || len(keys) == 0 {
		return delays, keys
	}
	var expanded []rune
	var expandedDelays []time.Duration
	for i, r := range keys {
		expanded = append(expanded, r)
		if i < len(delays) {
			expandedDelays = append(expandedDelays, delays[i])
		}
		if rng.Float64() < cfg.TypoRate {
			expanded = append(expanded, '\b')
			expanded = append(expanded, r)
			expandedDelays = append(expandedDelays, 80*time.Millisecond, delays[min(i, len(delays)-1)])
		}
	}
	return expandedDelays, expanded
}

// TotalTypingDuration sums rhythm delays.
func TotalTypingDuration(delays []time.Duration) time.Duration {
	var sum time.Duration
	for _, d := range delays {
		sum += d
	}
	return sum
}

// GaussianJitter applies sigma-scaled noise to base milliseconds.
func GaussianJitter(baseMs int, sigmaPct float64, rng *rand.Rand) int {
	if rng == nil {
		rng = rand.New(rand.NewPCG(11, 12))
	}
	if sigmaPct <= 0 {
		sigmaPct = 0.15
	}
	out := float64(baseMs) * (1 + sigmaPct*rng.NormFloat64())
	if out < 1 {
		return 1
	}
	return int(math.Round(out))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
