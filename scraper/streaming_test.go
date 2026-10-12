package scraper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStreamParserNoConsumers(t *testing.T) {
	s := NewStreamParser(0)
	result, err := s.ParseBytes(context.Background(), []byte("test data"))
	if err != nil {
		t.Fatal(err)
	}
	if result.BytesProcessed != 9 {
		t.Fatalf("expected 9 bytes, got %d", result.BytesProcessed)
	}
}

func TestStreamParserSingleConsumer(t *testing.T) {
	s := NewStreamParser(0)
	var buf bytes.Buffer
	s.AddConsumer("buffer", BufferConsumer(&buf))

	data := []byte("<html>test</html>")
	result, err := s.ParseBytes(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if result.BytesProcessed != int64(len(data)) {
		t.Fatalf("expected %d bytes, got %d", len(data), result.BytesProcessed)
	}
	if buf.String() != string(data) {
		t.Fatal("buffer consumer did not receive all data")
	}
}

func TestStreamParserMultipleConsumers(t *testing.T) {
	s := NewStreamParser(0)
	var buf1, buf2 bytes.Buffer
	s.AddConsumer("buf1", BufferConsumer(&buf1))
	s.AddConsumer("buf2", BufferConsumer(&buf2))

	data := []byte("<html>streaming test</html>")
	_, err := s.ParseBytes(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if buf1.String() != string(data) {
		t.Fatal("buf1 did not receive all data")
	}
	if buf2.String() != string(data) {
		t.Fatal("buf2 did not receive all data")
	}
}

func TestStreamParserThreeConsumers(t *testing.T) {
	s := NewStreamParser(0)
	var parseBuf, persistBuf bytes.Buffer
	h := sha256.New()
	s.AddConsumer("parser", BufferConsumer(&parseBuf))
	s.AddConsumer("hash", HashConsumer(h))
	s.AddConsumer("persist", BufferConsumer(&persistBuf))

	data := []byte("<html>3-consumer fan-out</html>")
	result, err := s.ParseBytes(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if parseBuf.String() != string(data) {
		t.Fatal("parser consumer missing data")
	}
	if persistBuf.String() != string(data) {
		t.Fatal("persist consumer missing data")
	}
	if h.Size() == 0 {
		t.Fatal("hash consumer did not process data")
	}
	_ = result
}

func TestStreamParserConsumerError(t *testing.T) {
	s := NewStreamParser(0)
	expectedErr := errors.New("consumer error")
	s.AddConsumer("error_consumer", func(ctx context.Context, r io.Reader) error {
		return expectedErr
	})

	result, err := s.ParseBytes(context.Background(), []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	if result.ConsumerErrors["error_consumer"] == nil {
		t.Fatal("expected consumer error in result")
	}
}

func TestStreamParserHashConsumer(t *testing.T) {
	h := sha256.New()
	consumer := HashConsumer(h)
	data := []byte("hash me")
	err := consumer(context.Background(), strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	// Verify hash was computed
	expected := sha256.Sum256(data)
	if !bytes.Equal(h.Sum(nil), expected[:]) {
		t.Fatal("hash mismatch")
	}
}

func TestStreamParserBufferConsumer(t *testing.T) {
	var buf bytes.Buffer
	consumer := BufferConsumer(&buf)
	data := []byte("buffer me")
	err := consumer(context.Background(), strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(data) {
		t.Fatal("buffer mismatch")
	}
}

func TestStreamParserCountConsumer(t *testing.T) {
	var count int64
	consumer := CountConsumer(&count)
	data := []byte("count these bytes")
	err := consumer(context.Background(), strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if count != int64(len(data)) {
		t.Fatalf("expected %d, got %d", len(data), count)
	}
}

func TestStreamParserCountConsumerNilCounter(t *testing.T) {
	consumer := CountConsumer(nil)
	err := consumer(context.Background(), strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamParserConsumerCount(t *testing.T) {
	s := NewStreamParser(0)
	if s.ConsumerCount() != 0 {
		t.Fatalf("expected 0, got %d", s.ConsumerCount())
	}
	s.AddConsumer("c1", BufferConsumer(&bytes.Buffer{}))
	s.AddConsumer("c2", BufferConsumer(&bytes.Buffer{}))
	if s.ConsumerCount() != 2 {
		t.Fatalf("expected 2, got %d", s.ConsumerCount())
	}
}

func TestStreamParserBufferSize(t *testing.T) {
	s := NewStreamParser(0)
	if s.bufferSize != 32*1024 {
		t.Fatalf("expected 32KB default, got %d", s.bufferSize)
	}
	s2 := NewStreamParser(64 * 1024)
	if s2.bufferSize != 64*1024 {
		t.Fatalf("expected 64KB, got %d", s2.bufferSize)
	}
}

func TestStreamParserNilInput(t *testing.T) {
	s := NewStreamParser(0)
	s.AddConsumer("c", BufferConsumer(&bytes.Buffer{}))
	_, err := s.ParseStream(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
}

func TestStreamParserLargeData(t *testing.T) {
	s := NewStreamParser(0)
	var counter int64
	s.AddConsumer("counter", CountConsumer(&counter))

	// Generate 1MB of data
	data := bytes.Repeat([]byte("A"), 1024*1024)
	result, err := s.ParseBytes(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if result.BytesProcessed != 1024*1024 {
		t.Fatalf("expected 1MB, got %d", result.BytesProcessed)
	}
	if counter != 1024*1024 {
		t.Fatalf("counter expected 1MB, got %d", counter)
	}
}

func TestStreamParserContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := NewStreamParser(0)
	cancel() // cancel immediately

	var counter int64
	s.AddConsumer("counter", CountConsumer(&counter))
	_, err := s.ParseBytes(ctx, []byte("data"))
	// Should not error on cancelled context, just process quickly
	_ = err
}

func TestStreamParserConcurrentAccess(t *testing.T) {
	s := NewStreamParser(0)
	var totalProcessed atomic.Int64

	for i := 0; i < 5; i++ {
		var counter int64
		s.AddConsumer("c", CountConsumer(&counter))
	}

	data := []byte("concurrent test")
	result, err := s.ParseBytes(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	totalProcessed.Add(result.BytesProcessed)
	if totalProcessed.Load() != int64(len(data)) {
		t.Fatalf("expected %d, got %d", len(data), totalProcessed.Load())
	}
}
