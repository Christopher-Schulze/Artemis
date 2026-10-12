package telemetry

import (
	"log/slog"
	"os"
	"runtime"
	"sync/atomic"
)

// PhoneHomeContract documents what an opt-out phone-home channel would
// transmit. No URLs, no headers, no content. Today the channel is a
// local-only contract: the values are gathered locally and emitted at info level via
// slog. Real transmission is intentionally deferred so embedders can
// inspect what would be sent.
type PhoneHomeContract struct {
	Version  string
	GOOS     string
	GOARCH   string
	Fetches  uint64
	Evals    uint64
	Errors   uint64
	Disabled bool
}

// PhoneHome aggregates the counters described in PhoneHomeContract.
type PhoneHome struct {
	logger   *slog.Logger
	disabled bool

	fetches atomic.Uint64
	evals   atomic.Uint64
	errors  atomic.Uint64

	version string
}

// NewPhoneHome builds a PhoneHome. The channel is disabled when the
// environment variable ARTEMIS_DISABLE_TELEMETRY is set to a truthy
// value.
func NewPhoneHome(version string, logger *slog.Logger) *PhoneHome {
	if logger == nil {
		logger = slog.Default()
	}
	return &PhoneHome{
		logger:   logger,
		disabled: phoneHomeDisabled(),
		version:  version,
	}
}

func phoneHomeDisabled() bool {
	v := os.Getenv("ARTEMIS_DISABLE_TELEMETRY")
	switch v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	}
	return false
}

// Disabled reports whether transmission is suppressed.
func (p *PhoneHome) Disabled() bool { return p.disabled }

// IncFetch increments the fetch counter.
func (p *PhoneHome) IncFetch() { p.fetches.Add(1) }

// IncEval increments the eval counter.
func (p *PhoneHome) IncEval() { p.evals.Add(1) }

// IncError increments the error counter.
func (p *PhoneHome) IncError() { p.errors.Add(1) }

// Snapshot reads the current counters.
func (p *PhoneHome) Snapshot() PhoneHomeContract {
	return PhoneHomeContract{
		Version:  p.version,
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Fetches:  p.fetches.Load(),
		Evals:    p.evals.Load(),
		Errors:   p.errors.Load(),
		Disabled: p.disabled,
	}
}

// Flush emits the contract as a structured log entry. Real transmission
// is intentionally deferred.
func (p *PhoneHome) Flush() {
	if p.disabled {
		return
	}
	c := p.Snapshot()
	p.logger.Info("telemetry phone_home local-only",
		"version", c.Version,
		"os", c.GOOS, "arch", c.GOARCH,
		"fetches", c.Fetches, "evals", c.Evals, "errors", c.Errors)
}
