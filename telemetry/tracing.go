// Package telemetry provides a thin tracing facade backed by slog. It
// exposes a Span API compatible in shape with OpenTelemetry so that the
// backend can be swapped without touching call sites.
package telemetry

import (
	"context"
	"log/slog"
	"time"
)

// Tracer creates Spans against a slog.Logger.
type Tracer struct {
	logger *slog.Logger
}

// NewTracer builds a Tracer. Nil logger uses slog.Default().
func NewTracer(logger *slog.Logger) *Tracer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracer{logger: logger}
}

// Span is one timed operation.
type Span struct {
	tracer *Tracer
	name   string
	start  time.Time
	attrs  []slog.Attr
	err    error
	ended  bool
}

// Span starts a new Span. Call End to emit.
func (t *Tracer) Span(_ context.Context, name string, attrs ...slog.Attr) *Span {
	return &Span{
		tracer: t,
		name:   name,
		start:  time.Now(),
		attrs:  attrs,
	}
}

// SetAttr appends an attribute.
func (s *Span) SetAttr(a slog.Attr) {
	s.attrs = append(s.attrs, a)
}

// Error attaches an error to the span; it will be logged at End.
func (s *Span) Error(err error) {
	if err == nil {
		return
	}
	s.err = err
}

// End emits the span as a single structured log line.
func (s *Span) End() {
	if s.ended {
		return
	}
	s.ended = true
	dur := time.Since(s.start)
	args := make([]any, 0, len(s.attrs)*2+4)
	args = append(args, "span", s.name, "duration_ms", dur.Milliseconds())
	for _, a := range s.attrs {
		args = append(args, a.Key, a.Value.Any())
	}
	if s.err != nil {
		args = append(args, "error", s.err.Error())
		s.tracer.logger.Error("span", args...)
		return
	}
	s.tracer.logger.Info("span", args...)
}
