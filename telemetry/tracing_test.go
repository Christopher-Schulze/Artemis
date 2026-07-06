package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSpanEmitsLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	tr := NewTracer(logger)
	sp := tr.Span(context.Background(), "fetch", slog.String("url", "https://e.test/"))
	sp.End()
	out := buf.String()
	if !strings.Contains(out, `span=fetch`) {
		t.Errorf("missing span name: %s", out)
	}
	if !strings.Contains(out, `url=https://e.test/`) {
		t.Errorf("missing attr: %s", out)
	}
	if !strings.Contains(out, "duration_ms=") {
		t.Errorf("missing duration: %s", out)
	}
}

func TestSpanError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tr := NewTracer(logger)
	sp := tr.Span(context.Background(), "eval")
	sp.Error(errors.New("boom"))
	sp.End()
	out := buf.String()
	if !strings.Contains(out, "error=boom") {
		t.Errorf("missing error: %s", out)
	}
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("error span should log at ERROR: %s", out)
	}
}

func TestSpanEndIdempotent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	tr := NewTracer(logger)
	sp := tr.Span(context.Background(), "x")
	sp.End()
	sp.End()
	if got := strings.Count(buf.String(), "span=x"); got != 1 {
		t.Errorf("emitted %d times, want 1", got)
	}
}
