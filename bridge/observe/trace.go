package observe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type TraceRecord struct {
	Schema       string   `json:"schema"`
	SnapshotHash string   `json:"snapshotHash"`
	Snapshot     Snapshot `json:"snapshot"`
}

// NewTraceRecord creates a deterministic, redacted observation evidence payload.
func NewTraceRecord(snapshot Snapshot) (TraceRecord, error) {
	if snapshot.Schema != Schema {
		return TraceRecord{}, fmt.Errorf("observation trace: unsupported schema %q", snapshot.Schema)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return TraceRecord{}, fmt.Errorf("observation trace: encode snapshot: %w", err)
	}
	digest := sha256.Sum256(raw)
	return TraceRecord{Schema: Schema, SnapshotHash: hex.EncodeToString(digest[:]), Snapshot: snapshot}, nil
}
