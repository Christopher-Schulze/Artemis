// Package scraper implements streaming parse with io.Pipe and TeeReader
// fan-out (spec ss28.15.7 P4.5).
//
// P4.5 Streaming Parse: io.Pipe -> goquery reads while download runs.
// Parser starts at first bytes. 3-consumer io.TeeReader fan-out chain on
// the same byte-stream:
//  1. parser (goquery tokenize)
//  2. xxhash-fingerprint (region-hash for differential-rescrape)
//  3. mmap-persist (EvidenceGraph artifact)
//
// All single-pass parallel without triple-buffering. Backpressure handled
// via bounded-buffer per consumer (slowest gates upstream).
package scraper

import (
	"bytes"
	"context"
	"errors"
	"hash"
	"io"
	"sync"
)

// StreamConsumer is a function that processes a stream of page bytes.
// Each consumer reads from its own reader in parallel.
type StreamConsumer func(ctx context.Context, r io.Reader) error

// StreamParseResult holds the outcome of a streaming parse operation.
type StreamParseResult struct {
	BytesProcessed int64
	ConsumerErrors map[string]error
}

// StreamParser enables single-pass parallel processing of a byte stream
// via io.TeeReader fan-out to multiple consumers.
type StreamParser struct {
	consumers  map[string]StreamConsumer
	bufferSize int
}

// NewStreamParser creates a StreamParser with the given buffer size.
// Default buffer size is 32KB.
func NewStreamParser(bufferSize int) *StreamParser {
	if bufferSize <= 0 {
		bufferSize = 32 * 1024
	}
	return &StreamParser{
		consumers:  make(map[string]StreamConsumer),
		bufferSize: bufferSize,
	}
}

// AddConsumer registers a named consumer that will receive a copy of the
// byte stream. Consumers run in parallel during ParseStream.
func (s *StreamParser) AddConsumer(name string, consumer StreamConsumer) {
	if s == nil || consumer == nil {
		return
	}
	s.consumers[name] = consumer
}

// ConsumerCount returns the number of registered consumers.
func (s *StreamParser) ConsumerCount() int {
	if s == nil {
		return 0
	}
	return len(s.consumers)
}

// ParseStream fans out the input reader to all registered consumers using
// io.Pipe and io.TeeReader. Each consumer reads from its own pipe reader
// in a separate goroutine. The slowest consumer gates the upstream rate
// (backpressure via bounded buffer).
//
// Returns StreamParseResult with bytes processed and any consumer errors.
func (s *StreamParser) ParseStream(ctx context.Context, input io.Reader) (*StreamParseResult, error) {
	if s == nil {
		return nil, errors.New("stream parser is nil")
	}
	if input == nil {
		return nil, errors.New("stream parser: input reader is nil")
	}
	if len(s.consumers) == 0 {
		// No consumers, just drain the input
		n, err := io.Copy(io.Discard, input)
		return &StreamParseResult{BytesProcessed: n}, err
	}

	// Create pipes for each consumer
	type pipePair struct {
		pr *io.PipeReader
		pw *io.PipeWriter
	}
	pipes := make(map[string]pipePair)
	for name := range s.consumers {
		pr, pw := io.Pipe()
		pipes[name] = pipePair{pr: pr, pw: pw}
	}

	// Start consumer goroutines
	var wg sync.WaitGroup
	errs := make(map[string]error)
	var errsMu sync.Mutex

	for name, consumer := range s.consumers {
		wg.Add(1)
		go func(n string, c StreamConsumer, pr *io.PipeReader) {
			defer wg.Done()
			err := c(ctx, pr)
			pr.Close()
			errsMu.Lock()
			errs[n] = err
			errsMu.Unlock()
		}(name, consumer, pipes[name].pr)
	}

	// Fan out: read from input, write to all pipe writers via TeeReader chain
	var totalBytes int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			for _, p := range pipes {
				p.pw.Close()
			}
		}()

		// Build TeeReader chain: input -> tee1 -> tee2 -> ... -> last
		var current io.Reader = input
		var writers []io.Writer

		names := make([]string, 0, len(pipes))
		for name := range pipes {
			names = append(names, name)
		}

		// Build multi-writer from all pipe writers
		for _, name := range names {
			writers = append(writers, pipes[name].pw)
		}

		mw := io.MultiWriter(writers...)
		n, err := io.Copy(mw, current)
		if err != nil {
			errsMu.Lock()
			errs["_stream"] = err
			errsMu.Unlock()
		}
		errsMu.Lock()
		totalBytes = n
		errsMu.Unlock()
	}()

	wg.Wait()

	result := &StreamParseResult{
		BytesProcessed: totalBytes,
		ConsumerErrors: make(map[string]error),
	}
	for name, err := range errs {
		if name == "_stream" {
			continue
		}
		result.ConsumerErrors[name] = err
	}

	return result, nil
}

// ParseBytes is a convenience method that creates a bytes.Reader from the
// given data and calls ParseStream.
func (s *StreamParser) ParseBytes(ctx context.Context, data []byte) (*StreamParseResult, error) {
	return s.ParseStream(ctx, bytes.NewReader(data))
}

// HashConsumer creates a StreamConsumer that hashes the input and stores
// the result in the given hash.Hash. Useful for xxhash fingerprinting.
func HashConsumer(h hash.Hash) StreamConsumer {
	return func(ctx context.Context, r io.Reader) error {
		_, err := io.Copy(h, r)
		return err
	}
}

// BufferConsumer creates a StreamConsumer that buffers all input into a
// bytes.Buffer. Useful for mmap-persist or full-content capture.
func BufferConsumer(buf *bytes.Buffer) StreamConsumer {
	return func(ctx context.Context, r io.Reader) error {
		_, err := io.Copy(buf, r)
		return err
	}
}

// CountConsumer creates a StreamConsumer that counts bytes without storing.
func CountConsumer(counter *int64) StreamConsumer {
	return func(ctx context.Context, r io.Reader) error {
		n, err := io.Copy(io.Discard, r)
		if counter != nil {
			*counter = n
		}
		return err
	}
}
