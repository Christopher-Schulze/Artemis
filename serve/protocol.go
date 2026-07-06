// Package serve implements the JSON-over-WebSocket steering protocol.
// This is NOT Chrome DevTools Protocol; it is a custom shape designed
// for agent-driven embedding of the Artemis engine.
package serve

import "encoding/json"

// Request is the incoming envelope.
type Request struct {
	ID     string          `json:"id"`
	Cmd    string          `json:"cmd"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the reply envelope.
type Response struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"`
	Error *Err   `json:"error,omitempty"`
}

// Err is a structured error returned in a Response.
type Err struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Event is a server-pushed message.
type Event struct {
	Event  string `json:"event"`
	Params any    `json:"params,omitempty"`
}
