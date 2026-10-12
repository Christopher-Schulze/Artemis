package network

import (
	"time"

	"github.com/Christopher-Schulze/Artemis/internal/random"
)

// TimingRandom is the random source required by request timing jitter.
type TimingRandom interface {
	Int64N(n int64) int64
}

// RequestTimingJitter sleeps for a randomized duration in [min,max].
func RequestTimingJitter(min, max time.Duration, rng TimingRandom) time.Duration {
	if min < 0 {
		min = 0
	}
	if max < min {
		max = min
	}
	if rng == nil {
		rng = random.Source{}
	}
	var d time.Duration
	if max == min {
		d = min
	} else {
		delta := max - min
		d = min + time.Duration(rng.Int64N(int64(delta)+1))
	}
	if d > 0 {
		time.Sleep(d)
	}
	return d
}
