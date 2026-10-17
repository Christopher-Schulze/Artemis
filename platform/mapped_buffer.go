package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

const MaxMappedBufferBytes = 64 << 20

var ErrMmapUnavailable = errors.New("anonymous mmap backend unavailable")

// MappedAllocationBytes is the page-rounded capacity reserved before mmap.
func MappedAllocationBytes(size int) (int64, error) {
	if size < 1 || size > MaxMappedBufferBytes {
		return 0, fmt.Errorf("mapped allocation size out of bounds")
	}
	pageSize := int64(os.Getpagesize())
	return ((int64(size) + pageSize - 1) / pageSize) * pageSize, nil
}

// MappedBuffer owns one anonymous mapping, without disk files or GC ownership.
// Borrowed bytes remain valid only until Close. Cancellation never unmaps bytes
// behind a running consumer; the owner must join work and then close the buffer.
type MappedBuffer struct {
	data []byte
	once sync.Once
	err  error
}

// NewMappedBuffer returns a non-nil owner with an error only if cancellation
// cleanup failed. The caller must retain that owner and report its Close error.
func NewMappedBuffer(ctx context.Context, size int) (*MappedBuffer, error) {
	if ctx == nil || size < 1 || size > MaxMappedBufferBytes {
		return nil, fmt.Errorf("mapped buffer: context and size within 1..%d required", MaxMappedBufferBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := mapBuffer(size)
	if err != nil {
		return nil, err
	}
	buffer := &MappedBuffer{data: data}
	if err := ctx.Err(); err != nil {
		if closeErr := buffer.Close(); closeErr != nil {
			return buffer, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return buffer, nil
}

// Bytes borrows the complete allocation. Do not race access with Close.
func (b *MappedBuffer) Bytes() []byte {
	if b == nil {
		return nil
	}
	return b.data
}

// Close releases the actual mapping once and preserves the release error.
func (b *MappedBuffer) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.err = unmapBuffer(b.data)
		if b.err == nil {
			b.data = nil
		}
	})
	return b.err
}
