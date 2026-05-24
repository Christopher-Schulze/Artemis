package network

import (
	"math/rand/v2"
	"time"
)

// RequestTimingJitter sleeps for a randomized duration in [min,max].
func RequestTimingJitter(min, max time.Duration, rng *rand.Rand) time.Duration {
	if min < 0 {
		min = 0
	}
	if max < min {
		max = min
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(9, 10))
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
