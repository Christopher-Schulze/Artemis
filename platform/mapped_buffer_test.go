package platform

import (
	"context"
	"errors"
	"testing"
)

func TestMappedBufferOwnsActualBytesAndJoinedClose(t *testing.T) {
	buffer, err := NewMappedBuffer(t.Context(), (1<<20)+19)
	if !Detect().HasMmap {
		if !errors.Is(err, ErrMmapUnavailable) || buffer != nil {
			t.Fatalf("unsupported mapping manufactured bytes: %v %v", buffer, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	if len(data) != (1<<20)+19 {
		t.Fatalf("mapping length %d", len(data))
	}
	data[0], data[len(data)-1] = 31, 97
	if buffer.Bytes()[0] != 31 || buffer.Bytes()[len(data)-1] != 97 {
		t.Fatal("mapping lost owned data")
	}
	if err := buffer.Close(); err != nil {
		t.Fatal(err)
	}
	if buffer.Bytes() != nil {
		t.Fatal("closed mapping still exposed bytes")
	}
	if err := buffer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMappedBufferRejectsBoundsAndCanceledAllocation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		ctx  context.Context
		size int
	}{
		{nil, 1}, {ctx, 1}, {t.Context(), 0}, {t.Context(), -1}, {t.Context(), MaxMappedBufferBytes + 1},
	} {
		if buffer, err := NewMappedBuffer(tc.ctx, tc.size); err == nil || buffer != nil {
			t.Fatalf("invalid mapping admitted: %v %v", buffer, err)
		}
	}
}
