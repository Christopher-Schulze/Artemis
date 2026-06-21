package scraper

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTeeParseReadersDuplicatesContent(t *testing.T) {
	primary := strings.NewReader("primary-body")
	secondary := strings.NewReader("secondary-body")
	p, s, err := TeeParseReaders(primary, secondary)
	if err != nil {
		t.Fatalf("TeeParseReaders: %v", err)
	}
	pb, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read primary: %v", err)
	}
	sb, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("read secondary: %v", err)
	}
	if string(pb) != "primary-body" {
		t.Fatalf("primary bytes mismatch: %q", string(pb))
	}
	if string(sb) != "secondary-body" {
		t.Fatalf("secondary bytes mismatch: %q", string(sb))
	}
}

func TestTeeParseReadersBothProduceSameContentOnReplay(t *testing.T) {
	primary := strings.NewReader("AAAA")
	secondary := strings.NewReader("BBBB")
	p, s, err := TeeParseReaders(primary, secondary)
	if err != nil {
		t.Fatalf("TeeParseReaders: %v", err)
	}
	// Read primary twice — first replay must yield AAAA, second must be EOF.
	first, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read primary first: %v", err)
	}
	if string(first) != "AAAA" {
		t.Fatalf("expected AAAA, got %q", string(first))
	}
	second, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read primary second: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("expected EOF on second read, got %q", string(second))
	}
	sec, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("read secondary: %v", err)
	}
	if string(sec) != "BBBB" {
		t.Fatalf("expected BBBB, got %q", string(sec))
	}
}

func TestTeeParseReadersNilPrimaryErrors(t *testing.T) {
	_, _, err := TeeParseReaders(nil, strings.NewReader("x"))
	if err == nil {
		t.Fatal("nil primary must error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestTeeParseReadersNilSecondaryErrors(t *testing.T) {
	_, _, err := TeeParseReaders(strings.NewReader("x"), nil)
	if err == nil {
		t.Fatal("nil secondary must error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestTeeParseReadersBothNilErrors(t *testing.T) {
	_, _, err := TeeParseReaders(nil, nil)
	if err == nil {
		t.Fatal("both nil must error")
	}
}

func TestTeeParseReadersEmptyInputs(t *testing.T) {
	p, s, err := TeeParseReaders(strings.NewReader(""), strings.NewReader(""))
	if err != nil {
		t.Fatalf("TeeParseReaders: %v", err)
	}
	pb, _ := io.ReadAll(p)
	sb, _ := io.ReadAll(s)
	if len(pb) != 0 || len(sb) != 0 {
		t.Fatalf("expected empty outputs, got %q / %q", string(pb), string(sb))
	}
}

func TestTeeParseReadersPrimaryReadErrorPropagates(t *testing.T) {
	errReader := &failingReader{failAt: 0}
	_, _, err := TeeParseReaders(errReader, strings.NewReader("ok"))
	if err == nil {
		t.Fatal("expected error from failing primary reader")
	}
}

func TestTeeParseReadersSecondaryReadErrorPropagates(t *testing.T) {
	errReader := &failingReader{failAt: 0}
	_, _, err := TeeParseReaders(strings.NewReader("ok"), errReader)
	if err == nil {
		t.Fatal("expected error from failing secondary reader")
	}
}

type failingReader struct {
	failAt int
	read   int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read >= r.failAt {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, []byte("x"))
	r.read += n
	return n, nil
}

// Sanity: ensure returned readers are independent byte buffers (no shared state).
func TestTeeParseReadersIndependentBuffers(t *testing.T) {
	primary := strings.NewReader("P")
	secondary := strings.NewReader("S")
	p, s, err := TeeParseReaders(primary, secondary)
	if err != nil {
		t.Fatalf("TeeParseReaders: %v", err)
	}
	// Drain primary fully, then secondary — must not affect each other.
	pb := make([]byte, 1)
	if _, err := p.Read(pb); err != nil {
		t.Fatalf("read p: %v", err)
	}
	if string(pb) != "P" {
		t.Fatalf("expected P, got %q", string(pb))
	}
	sb := make([]byte, 1)
	if _, err := s.Read(sb); err != nil {
		t.Fatalf("read s: %v", err)
	}
	if string(sb) != "S" {
		t.Fatalf("expected S, got %q", string(sb))
	}
	// Now drain remaining — both should be EOF.
	if _, err := p.Read(pb); err != io.EOF {
		t.Fatalf("expected EOF on second p read, got %v", err)
	}
	if _, err := s.Read(sb); err != io.EOF {
		t.Fatalf("expected EOF on second s read, got %v", err)
	}
}

// Ensure returned readers are NopClosers (so Close is a no-op).
func TestTeeParseReadersReturnsClosers(t *testing.T) {
	p, s, err := TeeParseReaders(strings.NewReader("a"), strings.NewReader("b"))
	if err != nil {
		t.Fatalf("TeeParseReaders: %v", err)
	}
	if c, ok := p.(io.Closer); !ok || c.Close() != nil {
		t.Fatal("primary must be a Closer with nil Close")
	}
	if c, ok := s.(io.Closer); !ok || c.Close() != nil {
		t.Fatal("secondary must be a Closer with nil Close")
	}
}

var _ = bytes.NewReader // keep bytes import alive for future expansion
