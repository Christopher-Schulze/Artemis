// Package serve implements the JSON-over-WebSocket steering protocol.
// This is NOT Chrome DevTools Protocol; it is a custom shape designed
// for agent-driven embedding of the Artemis engine.
package serve

import (
	"encoding/json"

	"github.com/Christopher-Schulze/Artemis/security"
)

// Request is the incoming envelope.
type Request struct {
	ID      string          `json:"id"`
	Version string          `json:"version,omitempty"`
	Cmd     string          `json:"cmd"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is the reply envelope.
type Response struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
	OK      bool   `json:"ok"`
	Value   any    `json:"value,omitempty"`
	Error   *Err   `json:"error,omitempty"`
	// Trust labels every page-derived value with its origin, source and
	// IDPI verdict (spec L4202); absent for non-page results.
	Trust *security.ContentTrust `json:"trust,omitempty"`
}

// Err is a structured error returned in a Response.
type Err struct {
	Code        string      `json:"code"`
	Message     string      `json:"message"`
	FailureKind FailureKind `json:"failureKind,omitempty"`
}

// FailureKind identifies an observed cause without guessing from message text.
type FailureKind string

const FailureNetwork FailureKind = "network"

func (e *Err) Error() string { return e.Code + ": " + e.Message }

// Event is a server-pushed message.
type Event struct {
	Event   string `json:"event"`
	Version string `json:"version,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
	Params  any    `json:"params,omitempty"`
	// Trust labels page-derived event params like Response.Trust.
	Trust *security.ContentTrust `json:"trust,omitempty"`
}
