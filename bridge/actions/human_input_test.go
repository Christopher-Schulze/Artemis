package actions

import (
	"strings"
	"testing"
	"time"
)

func TestHumanKeysExpandCorrectedTyposAndBoundDelays(t *testing.T) {
	profile := &HumanProfile{Seed: 7, KeyDelay: 60 * time.Millisecond, KeyVariance: 20 * time.Millisecond, TypoRate: 0.2}
	keys, delays := humanKeys(strings.Repeat("artemis ", 6), profile, profile.random())
	if len(keys) != len(delays) || len(keys) <= len("artemis ")*6 {
		t.Fatalf("typo rate produced no correction keystrokes: keys=%d delays=%d", len(keys), len(delays))
	}
	typed := []rune{}
	backspaces := 0
	for i, key := range keys {
		if key == '\b' {
			backspaces++
			if i == 0 || keys[i-1] == keys[i+1] {
				t.Fatalf("typo at %d is not a wrong key followed by Backspace and the intended key: %q", i, string(keys))
			}
			typed = typed[:len(typed)-1]
			continue
		}
		typed = append(typed, key)
	}
	if backspaces == 0 || string(typed) != strings.Repeat("artemis ", 6) {
		t.Fatalf("corrected typing does not reproduce the text: backspaces=%d typed=%q", backspaces, string(typed))
	}
	for _, delay := range delays {
		if delay < 40*time.Millisecond || delay > 80*time.Millisecond {
			t.Fatalf("keystroke delay %s escaped KeyDelay±KeyVariance", delay)
		}
	}
	again, _ := humanKeys(strings.Repeat("artemis ", 6), profile, profile.random())
	if string(again) != string(keys) {
		t.Fatal("seeded human typing is not reproducible")
	}
}

func TestHumanScrollDeltasAreEasedAndComplete(t *testing.T) {
	steps, interval := humanScrollDeltas(0, 1000, &HumanProfile{ScrollSteps: 10, ScrollDuration: 500 * time.Millisecond})
	if len(steps) < 8 || interval != 50*time.Millisecond {
		t.Fatalf("scroll collapsed instead of stepping: steps=%d interval=%s", len(steps), interval)
	}
	total := 0.0
	for _, step := range steps {
		total += step.Y
	}
	if total != 1000 || steps[0].Y >= steps[len(steps)/2].Y || steps[len(steps)-1].Y >= steps[len(steps)/2].Y {
		t.Fatalf("scroll steps are not an eased split of the full delta: %+v", steps)
	}
}

func TestHumanProfileRejectsUnboundedSettings(t *testing.T) {
	for _, profile := range []*HumanProfile{
		{KeyDelay: 3 * time.Second},
		{KeyDelay: 10 * time.Millisecond, KeyVariance: 20 * time.Millisecond},
		{TypoRate: 0.5},
		{ScrollSteps: 1000},
		{ScrollDuration: time.Minute},
		{PreClickDelay: -time.Millisecond},
	} {
		if profile.validate() == nil {
			t.Fatalf("unbounded profile admitted: %+v", profile)
		}
	}
}
