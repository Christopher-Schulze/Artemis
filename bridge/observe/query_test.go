package observe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func queryCollector(t *testing.T, caller *scriptedCaller) *Collector {
	t.Helper()
	collector, err := NewCollector(caller, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return collector
}

func requireQuery(t *testing.T, collector *Collector, request QueryRequest) QueryResult {
	t.Helper()
	result, err := collector.Query(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestQueryFullUnchangedAndMaterialDelta(t *testing.T) {
	caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}}
	collector := queryCollector(t, caller)
	first := requireQuery(t, collector, QueryRequest{})
	if first.Kind != QueryFull || first.Lost || len(first.Snapshot.Nodes) != 1 || first.Snapshot.Nodes[0].Ref == "" {
		t.Fatalf("invalid first projection: %+v", first)
	}
	first.Snapshot.Nodes[0].Name = "external mutation"
	unchanged := requireQuery(t, collector, QueryRequest{BaseRevision: first.Revision})
	if unchanged.Kind != QueryUnchanged || unchanged.Revision != first.Revision || unchanged.Snapshot != nil {
		t.Fatalf("baseline corrupted or unchanged lost: %+v", unchanged)
	}
	caller.hitBackend = 88
	delta := requireQuery(t, collector, QueryRequest{BaseRevision: first.Revision})
	if delta.Kind != QueryDelta || delta.Delta == nil || len(delta.Delta.Changed) != 1 || delta.Delta.Changed[0].Hit != HitCovered || delta.Delta.Changed[0].Interactable {
		t.Fatalf("actionability delta lost: %+v", delta)
	}
	if delta.Revision == first.Revision || delta.BaseRevision != first.Revision {
		t.Fatal("delta does not bind its exact base")
	}
	obsolete := requireQuery(t, collector, QueryRequest{BaseRevision: first.Revision})
	if obsolete.Kind != QueryFull || obsolete.ResetReason != "baseline_unavailable" || !obsolete.Lost {
		t.Fatalf("old revision of identical options accepted as incremental evidence: %+v", obsolete)
	}
}

func TestQueryOptionsNamespaceAndNavigationReset(t *testing.T) {
	caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false), domFixture(1, 4, "Login", false), domFixture(9, 8, "Login", false)}, roots: []int64{1, 1, 9}}
	collector := queryCollector(t, caller)
	first := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Roles: []string{"BUTTON", "button"}}})
	second := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Roles: []string{"button"}}, BaseRevision: first.Revision})
	if second.Kind != QueryUnchanged {
		t.Fatalf("equivalent normalized roles reset: %+v", second)
	}
	third := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Roles: []string{"button"}}, BaseRevision: second.Revision})
	if third.Kind != QueryFull || third.ResetReason != "navigation" || third.Lost || third.Epoch == second.Epoch {
		t.Fatalf("navigation accepted as delta: %+v", third)
	}
	changed := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Text: "Login"}, BaseRevision: third.Revision})
	if changed.Kind != QueryFull || changed.ResetReason != "options_changed" || !changed.Lost {
		t.Fatalf("wrong options accepted: %+v", changed)
	}
	foreign := requireQuery(t, collector, QueryRequest{BaseRevision: first.Revision})
	if foreign.Kind != QueryFull || foreign.ResetReason != "baseline_unavailable" || !foreign.Lost {
		t.Fatalf("stale baseline accepted: %+v", foreign)
	}
}

func TestQueryByteBudgetIncludesNamespaceAndNeverTruncatesDelta(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}})
	options := QueryOptions{MaxBytes: 700}
	first, err := collector.QueryWithNamespace(t.Context(), QueryRequest{Options: options}, strings.Repeat("p", 32)+":")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(first)
	if err != nil || len(raw) > options.MaxBytes || !first.Truncated || !first.Lost {
		t.Fatalf("unbounded or hidden namespace loss: bytes=%d result=%+v error=%v", len(raw), first, err)
	}
	second, err := collector.QueryWithNamespace(t.Context(), QueryRequest{Options: options, BaseRevision: first.Revision}, strings.Repeat("p", 32)+":")
	if err != nil || second.Kind != QueryFull || second.ResetReason != "incomplete_observation" || !second.Lost {
		t.Fatalf("truncated baseline accepted as delta: %+v err=%v", second, err)
	}
}

func TestQueryTargetDiagnosisAndCancelledLease(t *testing.T) {
	caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}}
	collector := queryCollector(t, caller)
	first := requireQuery(t, collector, QueryRequest{})
	ref := first.Snapshot.Nodes[0].Ref
	caller.hitBackend = 88
	covered := requireQuery(t, collector, QueryRequest{Options: QueryOptions{TargetRef: ref}})
	if covered.Target == nil || covered.Target.Status != ResolutionExact || covered.Target.State != ActionabilityCovered || len(covered.Target.Remedies) == 0 {
		t.Fatalf("covered target lacks actual diagnosis: %+v", covered)
	}
	stale := requireQuery(t, collector, QueryRequest{Options: QueryOptions{TargetRef: "e999999"}})
	if stale.Target == nil || stale.Target.Status != ResolutionStale || stale.Target.State != ActionabilityUnavailable {
		t.Fatalf("unknown target was invented: %+v", stale)
	}
	collector.queryLease <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := collector.Query(ctx, QueryRequest{}); err != context.Canceled {
		t.Fatalf("cancelled query waits for lease: %v", err)
	}
	<-collector.queryLease
}

func TestQueryFilteredHitUsesNativeUnemittedAncestry(t *testing.T) {
	document := domFixture(1, 4, "Login", false)
	nodes := &document.Documents[0].Nodes
	nodes.BackendNodeID = append(nodes.BackendNodeID, 77)
	nodes.ParentIndex = append(nodes.ParentIndex, 3)
	nodes.NodeName = append(nodes.NodeName, 3)
	nodes.NodeType = append(nodes.NodeType, 1)
	nodes.NodeValue = append(nodes.NodeValue, 2)
	nodes.Attributes = append(nodes.Attributes, nil)
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{document}, hitBackend: 77})
	result := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Roles: []string{"button"}}})
	if len(result.Snapshot.Nodes) != 1 || result.Snapshot.Nodes[0].Hit != HitClear || !result.Snapshot.Nodes[0].Interactable {
		t.Fatalf("filtered ancestry produced false coverage: %+v", result)
	}
	if _, _, err := collector.PointerPoint(t.Context(), result.Snapshot.Nodes[0]); err != nil {
		t.Fatalf("fresh target subtree could not verify omitted descendant: %v", err)
	}
}

func TestQueryRejectsInvalidScopeAndKeepsUTF8Boundaries(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}})
	for _, options := range []QueryOptions{{Mode: "invalid"}, {Mode: ModeSubtree}, {SubtreeRef: "e1"}, {MaxBytes: 1}, {MaxNodes: -1}, {Roles: []string{""}}, {FrameID: "foreign"}} {
		if _, err := collector.Query(t.Context(), QueryRequest{Options: options}); err == nil {
			t.Fatalf("invalid scope admitted: %+v", options)
		}
	}
	for i := range 10 {
		bounded := boundedUTF8("Ä文🙂end", i)
		if len(bounded) > i || !utf8.ValidString(bounded) {
			t.Fatalf("invalid bounded UTF-8 at %d: %q", i, bounded)
		}
	}
}

func TestQuerySubtreeAndReplacementDiagnosis(t *testing.T) {
	for _, test := range []struct {
		name, replacement string
		duplicate         bool
		status            ResolutionStatus
	}{
		{"detached", "Different", false, ResolutionDetached},
		{"ambiguous", "Login", true, ResolutionAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false), domFixture(1, 8, test.replacement, test.duplicate)}})
			first := requireQuery(t, collector, QueryRequest{})
			result := requireQuery(t, collector, QueryRequest{Options: QueryOptions{TargetRef: first.Snapshot.Nodes[0].Ref}})
			if result.Target == nil || result.Target.Status != test.status || len(result.Target.Remedies) == 0 || len(result.Target.Candidates) > 3 {
				t.Fatalf("replacement diagnosis invalid: %+v", result)
			}
		})
	}
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}})
	first := requireQuery(t, collector, QueryRequest{})
	result := requireQuery(t, collector, QueryRequest{Options: QueryOptions{Mode: ModeSubtree, SubtreeRef: first.Snapshot.Nodes[0].Ref}})
	if result.Snapshot == nil || len(result.Snapshot.Nodes) != 1 || result.Snapshot.Nodes[0].BackendNodeID != 4 {
		t.Fatalf("subtree scope escaped its root: %+v", result)
	}
	nodes := []Node{{FrameID: "main", BackendNodeID: 4}, {FrameID: "other", BackendNodeID: 4}, {FrameID: "main", BackendNodeID: 7, ParentBackendNodeID: 4}, {FrameID: "other", BackendNodeID: 7, ParentBackendNodeID: 4}}
	if scoped := filterSubtree(nodes, 4, "main"); len(scoped) != 2 || scoped[1].FrameID != "main" {
		t.Fatalf("colliding foreign backend escaped subtree: %+v", scoped)
	}
}

func TestQueryNamespaceChangesCannotAcceptBaseline(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}})
	first, err := collector.QueryWithNamespace(t.Context(), QueryRequest{}, "page-a:")
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.QueryWithNamespace(t.Context(), QueryRequest{BaseRevision: first.Revision}, "page-b:")
	if err != nil || second.Kind != QueryFull || second.ResetReason != "options_changed" || !second.Lost || !strings.HasPrefix(second.Snapshot.Nodes[0].Ref, "page-b:") {
		t.Fatalf("namespace change accepted as delta: %+v err=%v", second, err)
	}
}

func TestQueryDeltaBudgetReturnsCompleteReset(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true), domFixture(1, 8, "Login", true)}})
	// Measured complete full responses fit 1400 bytes; the four-node delta
	// requires 1547 bytes and must reset without weakening the node assertions.
	request := QueryRequest{Options: QueryOptions{MaxBytes: 1400}}
	first := requireQuery(t, collector, request)
	if first.Truncated || len(first.Snapshot.Nodes) != 2 {
		t.Fatalf("initial complete baseline required: %+v", first)
	}
	request.BaseRevision = first.Revision
	second := requireQuery(t, collector, request)
	raw, err := json.Marshal(second)
	if err != nil || len(raw) > request.Options.MaxBytes || second.Kind != QueryFull || second.Delta != nil || !second.Lost || second.ResetReason != "delta_budget" || second.Truncated || len(second.Snapshot.Nodes) != 2 {
		t.Fatalf("oversized delta masqueraded as valid incremental evidence: bytes=%d result=%+v err=%v", len(raw), second, err)
	}
}

func TestQueryExplicitTargetSurvivesNodeBudget(t *testing.T) {
	caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}}
	collector := queryCollector(t, caller)
	first := requireQuery(t, collector, QueryRequest{})
	ref := first.Snapshot.Nodes[1].Ref
	caller.hitBackend = 5
	result := requireQuery(t, collector, QueryRequest{Options: QueryOptions{TargetRef: ref, MaxNodes: 1}})
	if len(result.Snapshot.Nodes) != 1 || result.Snapshot.Nodes[0].BackendNodeID != 5 || result.Target.Status != ResolutionExact || result.Target.State != ActionabilityReady || !result.Truncated || !result.Lost {
		t.Fatalf("explicit late target disappeared behind equivalent matches: %+v", result)
	}
	_, err := collector.Query(t.Context(), QueryRequest{Options: QueryOptions{Mode: ModeSubtree, SubtreeRef: first.Snapshot.Nodes[0].Ref, TargetRef: ref}})
	if err == nil || !strings.Contains(err.Error(), "outside requested subtree") {
		t.Fatalf("scope contradiction reported as a detached target: %v", err)
	}
	_, err = collector.Query(t.Context(), QueryRequest{Options: QueryOptions{TargetRef: ref, MaxNodes: 1, MaxBytes: 512}})
	if err == nil || !strings.Contains(err.Error(), "required target evidence") {
		t.Fatalf("insufficient target bytes produced false success: %v", err)
	}
}

func TestQueryLeaseCancellationWhileWaiting(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}})
	collector.queryLease <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if ctx.Err() != nil {
		t.Fatal("query must start with a live context")
	}
	if _, err := collector.Query(ctx, QueryRequest{}); err != context.DeadlineExceeded {
		t.Fatalf("queued query did not respect cancellation: %v", err)
	}
	<-collector.queryLease
	if next := requireQuery(t, collector, QueryRequest{}); next.Kind != QueryFull {
		t.Fatalf("cancelled waiter corrupted next query: %+v", next)
	}
}

func TestQueryResultRejectsInconsistentEvidence(t *testing.T) {
	collector := queryCollector(t, &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}})
	first := requireQuery(t, collector, QueryRequest{})
	for _, test := range []struct {
		name   string
		mutate func(*QueryResult)
	}{
		{"snapshot schema", func(r *QueryResult) { r.Snapshot.Schema = "foreign" }},
		{"snapshot epoch", func(r *QueryResult) { r.Snapshot.Epoch++ }},
		{"node count", func(r *QueryResult) { r.NodeCount++ }},
		{"warnings", func(r *QueryResult) { r.Warnings = []string{"hidden failure"} }},
		{"lost evidence", func(r *QueryResult) { r.Truncated, r.Snapshot.Truncated = true, true }},
		{"timestamp", func(r *QueryResult) { r.CapturedAt = time.Time{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := first
			snapshot := cloneSnapshot(*first.Snapshot)
			result.Snapshot = &snapshot
			test.mutate(&result)
			if err := result.Validate(); err == nil {
				t.Fatalf("inconsistent evidence admitted: %+v", result)
			}
		})
	}
}

func TestQueryResolverCannotSelectFromIncompleteCandidates(t *testing.T) {
	caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false), domFixture(1, 8, "Login", true)}}
	config := DefaultConfig()
	config.MaxNodes = 1
	collector, err := NewCollector(caller, config)
	if err != nil {
		t.Fatal(err)
	}
	first := requireQuery(t, collector, QueryRequest{})
	resolved, err := collector.Resolve(t.Context(), first.Snapshot.Nodes[0].Ref)
	if err != nil || resolved.Status != ResolutionUnavailable || resolved.Node != nil {
		t.Fatalf("truncated equivalent candidates admitted semantic input target: %+v err=%v", resolved, err)
	}
}
