package serve

import (
	"encoding/json"
	"fmt"
	"time"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemisobserve "github.com/Christopher-Schulze/Artemis/observe"
)

// ObservationEvent serializes the canonical observation schema once for all stream consumers.
func ObservationEvent(snapshot bridgeobserve.Snapshot) (StreamEvent, error) {
	if snapshot.Schema != bridgeobserve.Schema {
		return StreamEvent{}, fmt.Errorf("snapshot event: unsupported schema %q", snapshot.Schema)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return StreamEvent{}, fmt.Errorf("snapshot event: encode: %w", err)
	}
	return StreamEvent{Type: StreamEventSnapshot, Data: raw, Timestamp: time.Now().UnixMilli()}, nil
}

// ObservationEvidenceEvent serializes the complete bounded live evidence
// projection for browser consumers that need network, console and metrics in
// addition to the stable DOM snapshot.
func ObservationEvidenceEvent(evidence artemisobserve.ObservationEvidence) (StreamEvent, error) {
	if evidence.Schema != bridgeobserve.Schema || evidence.Snapshot.Schema != bridgeobserve.Schema {
		return StreamEvent{}, fmt.Errorf("observation evidence event: unsupported schema %q", evidence.Schema)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return StreamEvent{}, fmt.Errorf("observation evidence event: encode: %w", err)
	}
	return StreamEvent{Type: StreamEventSnapshot, Data: raw, Timestamp: time.Now().UnixMilli()}, nil
}
