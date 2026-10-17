// Package pool owns the reusable byte buffers of Artemis' text dump paths
// (HTML, Text, Markdown). A Buffer keeps its backing array across Reset, so
// a returned buffer really saves the growth allocations of the next dump.
// Output leaves only through String, which copies, so no result aliases
// pooled memory. PutBuffer clears the bytes (page content is customer data)
// and drops buffers above MaxRetainedBytes.
package pool

import (
	"sync"
	"unicode/utf8"
)

// MaxRetainedBytes bounds the capacity a returned buffer may keep.
const MaxRetainedBytes = 4 << 20

// Buffer is an append-only byte buffer that keeps capacity across Reset.
type Buffer struct {
	data []byte
	// written is the high-water mark of bytes written since the last Wipe,
	// so Wipe clears exactly the bytes that ever held content.
	written int
}

var bufferPool = sync.Pool{
	New: func() any { return &Buffer{} },
}

// GetBuffer returns an empty buffer, reusing retained capacity when present.
func GetBuffer() *Buffer {
	return bufferPool.Get().(*Buffer)
}

// PutBuffer clears b and returns it to the pool unless it exceeds
// MaxRetainedBytes. b must not be used afterwards.
func PutBuffer(b *Buffer) {
	if b == nil {
		return
	}
	if cap(b.data) > MaxRetainedBytes {
		return
	}
	b.Wipe()
	bufferPool.Put(b)
}

// Wipe zeroes the whole backing array and empties the buffer, for owners
// that retain a Buffer outside the pool.
func (b *Buffer) Wipe() {
	b.mark()
	clear(b.data[:b.written])
	b.data, b.written = b.data[:0], 0
}

func (b *Buffer) mark() {
	if len(b.data) > b.written {
		b.written = len(b.data)
	}
}

// Write appends p.
func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// WriteString appends s.
func (b *Buffer) WriteString(s string) (int, error) {
	b.data = append(b.data, s...)
	return len(s), nil
}

// WriteByte appends c.
func (b *Buffer) WriteByte(c byte) error {
	b.data = append(b.data, c)
	return nil
}

// WriteRune appends the UTF-8 encoding of r.
func (b *Buffer) WriteRune(r rune) (int, error) {
	before := len(b.data)
	b.data = utf8.AppendRune(b.data, r)
	return len(b.data) - before, nil
}

// Grow ensures room for n more bytes.
func (b *Buffer) Grow(n int) {
	if n > cap(b.data)-len(b.data) {
		b.mark()
		grown := make([]byte, len(b.data), len(b.data)+n)
		copy(grown, b.data)
		clear(b.data[:b.written])
		b.data, b.written = grown, len(grown)
	}
}

// Len returns the number of buffered bytes.
func (b *Buffer) Len() int {
	return len(b.data)
}

// Cap returns the retained capacity.
func (b *Buffer) Cap() int {
	return cap(b.data)
}

// Bytes returns the buffered bytes without copying. The slice is valid only
// until the next write, Reset or PutBuffer and must not escape the caller.
func (b *Buffer) Bytes() []byte {
	return b.data
}

// String returns a copy of the buffered bytes.
func (b *Buffer) String() string {
	return string(b.data)
}

// HasSuffix reports whether the buffered bytes end with suffix.
func (b *Buffer) HasSuffix(suffix string) bool {
	return len(b.data) >= len(suffix) && string(b.data[len(b.data)-len(suffix):]) == suffix
}

// Reset empties the buffer and keeps its capacity; the bytes stay until
// Wipe or PutBuffer.
func (b *Buffer) Reset() {
	b.mark()
	b.data = b.data[:0]
}
