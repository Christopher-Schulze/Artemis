package random

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math"
)

// Source provides cryptographically secure random values for low-volume
// human-input and timing variation. Its zero value is ready for use.
type Source struct{}

// Uint64 returns a cryptographically secure 64-bit value.
func (Source) Uint64() uint64 {
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return 0
	}
	return binary.LittleEndian.Uint64(raw[:])
}

// Float64 returns a value in [0, 1).
func (s Source) Float64() float64 {
	return float64(s.Uint64()>>11) * (1.0 / float64(uint64(1)<<53))
}

// NormFloat64 returns a normally distributed value with mean zero and unit
// variance using the Box-Muller transform.
func (s Source) NormFloat64() float64 {
	const denominator = float64(uint64(1) << 53)
	u1 := (float64(s.Uint64()>>11) + 1) / (denominator + 1)
	u2 := float64(s.Uint64()>>11) / denominator
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// IntN returns a uniformly distributed value in [0, n). Non-positive bounds
// return zero because callers use validated, positive ranges.
func (s Source) IntN(n int) int {
	if n <= 0 {
		return 0
	}
	bound := uint64(n)
	limit := ^uint64(0) - (^uint64(0) % bound)
	for {
		value := s.Uint64()
		if value < limit {
			return int(value % bound)
		}
	}
}

// Intn is the spelling used by math/rand-compatible callers.
func (s Source) Intn(n int) int {
	return s.IntN(n)
}

// Int64N returns a uniformly distributed value in [0, n). Non-positive
// bounds return zero because callers use validated, positive ranges.
func (s Source) Int64N(n int64) int64 {
	if n <= 0 {
		return 0
	}
	bound := uint64(n)
	limit := ^uint64(0) - (^uint64(0) % bound)
	for {
		value := s.Uint64()
		if value < limit {
			return int64(value % bound)
		}
	}
}
