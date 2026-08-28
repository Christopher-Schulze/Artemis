package random

import (
	"math"
	"testing"
)

func TestSourceIntNBounds(t *testing.T) {
	source := Source{}
	maxInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name string
		n    int
	}{
		{name: "negative", n: -1},
		{name: "zero", n: 0},
		{name: "one", n: 1},
		{name: "small", n: 17},
		{name: "maximum", n: maxInt},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := 0; i < 256; i++ {
				got := source.IntN(test.n)
				if test.n <= 0 {
					if got != 0 {
						t.Fatalf("IntN(%d) = %d, want 0", test.n, got)
					}
					continue
				}
				if got < 0 || got >= test.n {
					t.Fatalf("IntN(%d) = %d, want [0, %d)", test.n, got, test.n)
				}
			}
		})
	}
}

func TestSourceInt64NBounds(t *testing.T) {
	source := Source{}
	for _, test := range []struct {
		name string
		n    int64
	}{
		{name: "negative", n: -1},
		{name: "zero", n: 0},
		{name: "one", n: 1},
		{name: "small", n: 17},
		{name: "maximum", n: math.MaxInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := 0; i < 256; i++ {
				got := source.Int64N(test.n)
				if test.n <= 0 {
					if got != 0 {
						t.Fatalf("Int64N(%d) = %d, want 0", test.n, got)
					}
					continue
				}
				if got < 0 || got >= test.n {
					t.Fatalf("Int64N(%d) = %d, want [0, %d)", test.n, got, test.n)
				}
			}
		})
	}
}

func TestSourceIntnAliasBounds(t *testing.T) {
	source := Source{}
	for i := 0; i < 256; i++ {
		got := source.Intn(31)
		if got < 0 || got >= 31 {
			t.Fatalf("Intn(31) = %d, want [0, 31)", got)
		}
	}
}

func TestSourceFloatShapes(t *testing.T) {
	source := Source{}
	for i := 0; i < 256; i++ {
		uniform := source.Float64()
		if uniform < 0 || uniform >= 1 || math.IsNaN(uniform) {
			t.Fatalf("Float64() = %v, want a value in [0, 1)", uniform)
		}
		normal := source.NormFloat64()
		if math.IsNaN(normal) || math.IsInf(normal, 0) {
			t.Fatalf("NormFloat64() = %v, want a finite value", normal)
		}
	}
}
