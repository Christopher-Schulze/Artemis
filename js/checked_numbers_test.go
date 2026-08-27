package js

import (
	"math"
	"strconv"
	"testing"
)

func TestCheckedInt64ToUint32(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  uint32
		ok    bool
	}{
		{name: "negative", input: -1},
		{name: "zero", input: 0, want: 0, ok: true},
		{name: "maximum", input: math.MaxUint32, want: math.MaxUint32, ok: true},
		{name: "overflow", input: math.MaxUint32 + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedInt64ToUint32(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedInt64ToUint32(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCheckedUint32ToInt32(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input uint32
		want  int32
		ok    bool
	}{
		{name: "zero", input: 0, want: 0, ok: true},
		{name: "maximum", input: math.MaxInt32, want: math.MaxInt32, ok: true},
		{name: "overflow", input: math.MaxInt32 + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedUint32ToInt32(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedUint32ToInt32(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCheckedIntToInt32(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input int
		want  int32
		ok    bool
	}{
		{name: "minimum", input: math.MinInt32, want: math.MinInt32, ok: true},
		{name: "maximum", input: math.MaxInt32, want: math.MaxInt32, ok: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedIntToInt32(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedIntToInt32(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
	if strconv.IntSize > 32 {
		got, ok := checkedIntToInt32(-int(math.MaxInt32) - 2)
		if got != 0 || ok {
			t.Fatalf("negative overflow = (%d, %t), want (0, false)", got, ok)
		}
		got, ok = checkedIntToInt32(int(math.MaxInt32) + 1)
		if got != 0 || ok {
			t.Fatalf("positive overflow = (%d, %t), want (0, false)", got, ok)
		}
	}
}

func TestCheckedIntToUint32(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input int
		want  uint32
		ok    bool
	}{
		{name: "negative", input: -1},
		{name: "zero", input: 0, want: 0, ok: true},
		{name: "maximum signed 32-bit", input: math.MaxInt32, want: math.MaxInt32, ok: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedIntToUint32(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedIntToUint32(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
	if strconv.IntSize > 32 {
		got, ok := checkedIntToUint32(int(math.MaxUint32))
		if got != math.MaxUint32 || !ok {
			t.Fatalf("maximum = (%d, %t), want (%d, true)", got, ok, uint32(math.MaxUint32))
		}
		got, ok = checkedIntToUint32(int(math.MaxUint32) + 1)
		if got != 0 || ok {
			t.Fatalf("overflow = (%d, %t), want (0, false)", got, ok)
		}
	}
}

func TestCheckedInt64ToByte(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input int64
		want  byte
		ok    bool
	}{
		{name: "negative", input: -1},
		{name: "zero", input: 0, want: 0, ok: true},
		{name: "maximum", input: math.MaxUint8, want: math.MaxUint8, ok: true},
		{name: "overflow", input: math.MaxUint8 + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedInt64ToByte(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedInt64ToByte(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCheckedInt64ToUint16(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input int64
		want  uint16
		ok    bool
	}{
		{name: "negative", input: -1},
		{name: "zero", input: 0, want: 0, ok: true},
		{name: "maximum", input: math.MaxUint16, want: math.MaxUint16, ok: true},
		{name: "overflow", input: math.MaxUint16 + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedInt64ToUint16(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("checkedInt64ToUint16(%d) = (%d, %t), want (%d, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}
