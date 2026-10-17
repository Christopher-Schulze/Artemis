package observe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"
)

const QuerySchema = "artemis.query.v1"

type queryBaseline struct {
	revision, options string
	snapshot          Snapshot
}

// Query returns a bounded projection using this collector's canonical refs.
func (c *Collector) Query(ctx context.Context, request QueryRequest) (QueryResult, error) {
	return c.QueryWithNamespace(ctx, request, "")
}

// QueryWithNamespace applies the owning page's presentation prefix before
// byte accounting and revision binding; it never creates another ref owner.
func (c *Collector) QueryWithNamespace(ctx context.Context, request QueryRequest, namespace string) (QueryResult, error) {
	if c == nil || c.queryLease == nil || ctx == nil {
		return QueryResult{}, fmt.Errorf("query: initialized collector and context required")
	}
	if err := ctx.Err(); err != nil {
		return QueryResult{}, err
	}
	if len(request.BaseRevision) > 64 || len(namespace) > 128 || !utf8.ValidString(namespace) {
		return QueryResult{}, fmt.Errorf("query: revision/namespace exceeds bounds")
	}
	options, err := c.normalizeQuery(request.Options)
	if err != nil {
		return QueryResult{}, err
	}
	select {
	case c.queryLease <- struct{}{}:
		defer func() { <-c.queryLease }()
	case <-ctx.Done():
		return QueryResult{}, ctx.Err()
	}
	scope, err := c.newNodeScope(options)
	if err != nil {
		return QueryResult{}, err
	}
	snapshot, err := c.captureSelected(ctx, ModeFull, "", scope)
	if err != nil {
		return QueryResult{}, err
	}
	target := c.diagnoseTarget(snapshot, options.TargetRef, scope.target)
	bindQueryNamespace(&snapshot, target, namespace)
	return c.publishQuery(snapshot, target, options, request.BaseRevision, namespace)
}

func (c *Collector) publishQuery(snapshot Snapshot, target *TargetDiagnosis, options QueryOptions, base, namespace string) (QueryResult, error) {
	signature, err := querySignature(options, namespace)
	if err != nil {
		return QueryResult{}, err
	}
	result := QueryResult{Schema: QuerySchema, Kind: QueryFull, BaseRevision: base, Epoch: snapshot.Epoch, CapturedAt: snapshot.CapturedAt, NodeCount: snapshot.TotalNodes, Snapshot: &snapshot, Target: target, Truncated: snapshot.Truncated, TruncationReasons: slices.Clone(snapshot.TruncationReasons), Warnings: slices.Clone(snapshot.Warnings), Lost: snapshot.Truncated}
	baseline := c.queryBaseline
	if base != "" {
		result.ResetReason = queryResetReason(baseline, snapshot, signature, base)
		result.Lost = result.Lost || result.ResetReason != "" && result.ResetReason != "navigation"
		if result.ResetReason == "" {
			diff := DiffSnapshots(baseline.snapshot, snapshot)
			result.Snapshot = nil
			result.Kind, result.Delta = QueryDelta, &diff
			if len(diff.Added)+len(diff.Changed)+len(diff.Removed) == 0 && slices.Equal(baseline.snapshot.Warnings, snapshot.Warnings) {
				result.Kind, result.Delta, result.Revision = QueryUnchanged, nil, baseline.revision
			}
		}
	}
	if result.Revision == "" {
		result.Revision, err = newQueryRevision()
		if err != nil {
			return QueryResult{}, err
		}
	}
	return c.boundAndStoreQuery(result, snapshot, signature, options.MaxBytes)
}

func queryResetReason(baseline *queryBaseline, snapshot Snapshot, signature, revision string) string {
	if baseline == nil || baseline.revision != revision {
		return "baseline_unavailable"
	}
	if baseline.options != signature {
		return "options_changed"
	}
	if baseline.snapshot.Epoch != snapshot.Epoch {
		return "navigation"
	}
	if baseline.snapshot.Truncated || snapshot.Truncated {
		return "incomplete_observation"
	}
	return ""
}

func (c *Collector) boundAndStoreQuery(result QueryResult, snapshot Snapshot, signature string, limit int) (QueryResult, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query: encode response: %w", err)
	}
	if len(raw) > limit && result.Kind != QueryFull {
		result.Kind, result.Delta, result.Snapshot = QueryFull, nil, &snapshot
		result.Lost, result.ResetReason = true, "delta_budget"
	}
	for {
		raw, err = json.Marshal(result)
		if err != nil {
			return QueryResult{}, fmt.Errorf("query: encode bounded response: %w", err)
		}
		if len(raw) <= limit {
			break
		}
		if result.Snapshot == nil || len(result.Snapshot.Nodes) == 0 {
			return QueryResult{}, fmt.Errorf("query: byte budget cannot fit required observation metadata")
		}
		fit := len(result.Snapshot.Nodes) * limit / len(raw)
		if fit >= len(result.Snapshot.Nodes) {
			fit = len(result.Snapshot.Nodes) - 1
		}
		if result.Target != nil && result.Target.Status == ResolutionExact && result.Snapshot.Nodes[0].Ref == result.Target.Ref && fit == 0 {
			if len(result.Snapshot.Nodes) == 1 {
				return QueryResult{}, fmt.Errorf("query: byte budget cannot fit required target evidence")
			}
			fit = 1
		}
		result.Snapshot.Nodes = result.Snapshot.Nodes[:fit]
		result.Snapshot.Truncated, result.Truncated, result.Lost = true, true, true
		if !contains(result.Snapshot.TruncationReasons, "query_max_bytes") {
			result.Snapshot.TruncationReasons = append(result.Snapshot.TruncationReasons, "query_max_bytes")
			result.TruncationReasons = append(result.TruncationReasons, "query_max_bytes")
		}
	}
	stored := snapshot
	if result.Snapshot != nil {
		stored = *result.Snapshot
	}
	encoded, err := json.Marshal(stored)
	if err != nil || len(encoded) > c.config.MaxSnapshotBytes {
		return QueryResult{}, fmt.Errorf("query: baseline payload exceeds collector budget")
	}
	c.queryBaseline = &queryBaseline{revision: result.Revision, options: signature, snapshot: cloneSnapshot(stored)}
	return result, nil
}

func querySignature(options QueryOptions, namespace string) (string, error) {
	raw, err := json.Marshal(struct {
		Options   QueryOptions `json:"options"`
		Namespace string       `json:"namespace"`
	}{options, namespace})
	if err != nil {
		return "", fmt.Errorf("query: encode options: %w", err)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func newQueryRevision() (string, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", fmt.Errorf("query: revision identity: %w", err)
	}
	return hex.EncodeToString(identity[:]), nil
}

func bindQueryNamespace(snapshot *Snapshot, target *TargetDiagnosis, namespace string) {
	for i := range snapshot.Nodes {
		if snapshot.Nodes[i].Ref != "" {
			snapshot.Nodes[i].Ref = namespace + snapshot.Nodes[i].Ref
		}
	}
	if target == nil {
		return
	}
	target.Ref = namespace + target.Ref
	for i := range target.Candidates {
		target.Candidates[i].Ref = namespace + target.Candidates[i].Ref
	}
}
