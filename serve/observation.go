package serve

import (
	"encoding/json"
	"fmt"
	"time"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
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
