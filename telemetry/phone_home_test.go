package telemetry

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestPhoneHomeCounters(t *testing.T) {
	t.Setenv("ARTEMIS_DISABLE_TELEMETRY", "")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	p := NewPhoneHome("0.0.1-test", logger)
	if p.Disabled() {
		t.Fatal("not disabled by default")
	}
	p.IncFetch()
	p.IncFetch()
	p.IncEval()
	p.IncError()
	p.Flush()
	out := buf.String()
	for _, want := range []string{"fetches=2", "evals=1", "errors=1", "version=0.0.1-test"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in: %s", want, out)
		}
	}
}

func TestPhoneHomeDisabled(t *testing.T) {
	t.Setenv("ARTEMIS_DISABLE_TELEMETRY", "1")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	p := NewPhoneHome("0.0.1", logger)
	if !p.Disabled() {
		t.Fatal("expected disabled")
	}
	p.IncFetch()
	p.Flush()
	if buf.Len() != 0 {
		t.Errorf("disabled flush should not emit, got: %s", buf.String())
	}
}
