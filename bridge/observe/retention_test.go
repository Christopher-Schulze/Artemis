package observe

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func assertReferenceBounds(t *testing.T, collector *Collector) {
	t.Helper()
	if len(collector.refs) != len(collector.identities) || len(collector.refs) > collector.config.MaxReferences || collector.referenceBytes > collector.config.MaxReferenceBytes {
		t.Fatalf("reference bounds: refs=%d identities=%d bytes=%d config=%+v", len(collector.refs), len(collector.identities), collector.referenceBytes, collector.config)
	}
	seen := make(map[*remembered]bool)
	var previous *remembered
	bytes := 0
	for entry := collector.oldest; entry != nil; entry = entry.newer {
		if seen[entry] || entry.older != previous || collector.refs[entry.ref] != entry {
			t.Fatalf("broken reference recency chain at %q", entry.ref)
		}
		seen[entry], previous = true, entry
		raw, err := json.Marshal(entry.node)
		if err != nil || len(raw) != entry.bytes {
			t.Fatalf("incorrect encoded reference accounting: ref=%s bytes=%d encoded=%d err=%v", entry.ref, entry.bytes, len(raw), err)
		}
		bytes += len(raw)
		key := identity{epoch: entry.epoch, frame: entry.node.FrameID, backend: entry.node.BackendNodeID}
		if collector.identities[key] != entry.ref || collector.epoch-entry.epoch >= uint64(collector.config.RetainedEpochs) {
			t.Fatalf("wrong/expired reference identity: %+v", key)
		}
	}
	if len(seen) != len(collector.refs) || previous != collector.newest || bytes != collector.referenceBytes {
		t.Fatalf("reference chain/accounting mismatch: chain=%d refs=%d bytes=%d accounted=%d", len(seen), len(collector.refs), bytes, collector.referenceBytes)
	}
}

func TestReferenceRetentionBoundsNavigationAndMutationChurn(t *testing.T) {
	for _, navigations := range []bool{false, true} {
		t.Run(strconv.FormatBool(navigations), func(t *testing.T) {
			caller := &scriptedCaller{}
			for i := range 512 {
				root := int64(1)
				if navigations {
					root = int64(1000 + i)
				}
				caller.documents = append(caller.documents, domFixture(root, int64(10000+i), "Login", false))
				caller.roots = append(caller.roots, root)
			}
			config := DefaultConfig()
			config.MaxReferences, config.RetainedEpochs = 7, 3
			collector, err := NewCollector(caller, config)
			if err != nil {
				t.Fatal(err)
			}
			var firstRef string
			issued := make(map[string]bool)
			for i := range 512 {
				snapshot, err := collector.Capture(t.Context(), ModeFull, "")
				if err != nil {
					t.Fatal(err)
				}
				ref := findBackend(t, snapshot.Nodes, int64(10000+i)).Ref
				if ref == "" || issued[ref] {
					t.Fatalf("reference absent/recycled at capture %d: %q", i, ref)
				}
				issued[ref] = true
				if i == 0 {
					firstRef = ref
				}
				assertReferenceBounds(t, collector)
			}
			resolution, err := collector.Resolve(t.Context(), firstRef)
			if err != nil || resolution.Status != ResolutionStale || resolution.Node != nil {
				t.Fatalf("evicted ref aliased a later semantic match: %+v error=%v", resolution, err)
			}
		})
	}
}

func TestReferenceEvictionDuringResolveIsStale(t *testing.T) {
	for _, epochs := range []int{1, 2} {
		t.Run(strconv.Itoa(epochs), func(t *testing.T) {
			caller := &scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false), domFixture(9, 8, "Login", false)}, roots: []int64{1, 9}}
			config := DefaultConfig()
			config.RetainedEpochs = epochs
			collector, err := NewCollector(caller, config)
			if err != nil {
				t.Fatal(err)
			}
			before, err := collector.Capture(t.Context(), ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			resolution, err := collector.Resolve(t.Context(), findBackend(t, before.Nodes, 4).Ref)
			if err != nil {
				t.Fatal(err)
			}
			if epochs == 1 && (resolution.Status != ResolutionStale || resolution.Node != nil || !strings.Contains(resolution.Reason, "evicted")) {
				t.Fatalf("expired during capture was re-resolved: %+v", resolution)
			}
			if epochs == 2 && (resolution.Status != ResolutionReResolved || resolution.Node == nil || resolution.Node.BackendNodeID != 8) {
				t.Fatalf("retained semantic reference lost: %+v", resolution)
			}
			assertReferenceBounds(t, collector)
		})
	}
}

func TestRecencyPreservesLiveReferencesDuringSiblingChurn(t *testing.T) {
	caller := &scriptedCaller{}
	for i := range 40 {
		document := domFixture(1, 4, "Login", true)
		document.Documents[0].Nodes.BackendNodeID[4] = int64(100 + i)
		caller.documents = append(caller.documents, document)
	}
	config := DefaultConfig()
	config.MaxReferences = 3
	collector, err := NewCollector(caller, config)
	if err != nil {
		t.Fatal(err)
	}
	var liveRef string
	for range 40 {
		snapshot, err := collector.Capture(t.Context(), ModeFull, "")
		if err != nil {
			t.Fatal(err)
		}
		ref := findBackend(t, snapshot.Nodes, 4).Ref
		if liveRef == "" {
			liveRef = ref
		}
		if ref == "" || ref != liveRef {
			t.Fatalf("continuously observed ref evicted/recycled: %q expected %q", ref, liveRef)
		}
		assertReferenceBounds(t, collector)
	}
}

func TestOversizedReferenceUpdateReleasesIdentityWithoutAliasing(t *testing.T) {
	large := domFixture(1, 4, "Login", false)
	large.Strings = append(large.Strings, "data-extra", strings.Repeat("x", 20000))
	large.Documents[0].Nodes.Attributes[3] = append(large.Documents[0].Nodes.Attributes[3], stringIndex(len(large.Strings)-2), stringIndex(len(large.Strings)-1))
	config := DefaultConfig()
	config.MaxReferenceBytes = 1000
	collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false), large, domFixture(1, 4, "Login", false)}}, config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := collector.Capture(t.Context(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	old := findBackend(t, first.Nodes, 4).Ref
	second, err := collector.Capture(t.Context(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	if findBackend(t, second.Nodes, 4).Ref != "" || !contains(second.TruncationReasons, "reference_retention") {
		t.Fatalf("oversized updated payload stayed referenceable: %+v", second)
	}
	assertReferenceBounds(t, collector)
	third, err := collector.Capture(t.Context(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	if ref := findBackend(t, third.Nodes, 4).Ref; ref == "" || ref == old {
		t.Fatalf("returned small node aliases an evicted ref: %q old=%q", ref, old)
	}
	if result, err := collector.Resolve(t.Context(), old); err != nil || result.Status != ResolutionStale || result.Node != nil {
		t.Fatalf("oversized old identity reappeared: %+v err=%v", result, err)
	}
	assertReferenceBounds(t, collector)
}

func TestReferencePayloadAndCountOmissionsAreExplicit(t *testing.T) {
	probe, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Capture(t.Context(), ModeFull, ""); err != nil {
		t.Fatal(err)
	}
	largestPayload, totalPayload := 0, 0
	for _, entry := range probe.refs {
		largestPayload = max(largestPayload, entry.bytes)
		totalPayload += entry.bytes
	}
	if largestPayload == 0 || totalPayload <= largestPayload {
		t.Fatal("fixture must contain independently measurable reference payloads")
	}
	for _, test := range []struct {
		name              string
		references, bytes int
	}{
		{"count", 1, 1 << 20},
		{"bytes", 30, largestPayload},
		{"oversized", 30, 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaxReferences, config.MaxReferenceBytes = test.references, test.bytes
			collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}}, config)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := collector.Capture(t.Context(), ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			if !snapshot.Truncated || !contains(snapshot.TruncationReasons, "reference_retention") {
				t.Fatalf("reference omission hidden: %+v", snapshot)
			}
			for _, node := range snapshot.Nodes {
				if node.Ref != "" && collector.refs[node.Ref] == nil {
					t.Fatalf("published an unusable reference: %+v", node)
				}
			}
			if test.name == "oversized" && len(collector.refs) != 0 {
				t.Fatal("oversized node retained")
			}
			if test.name == "bytes" && len(collector.refs) != 1 {
				t.Fatalf("measured payload budget did not retain exactly one entry: %d", len(collector.refs))
			}
			assertReferenceBounds(t, collector)
		})
	}
}

func TestReferenceConfigurationAndExhaustionFailClosed(t *testing.T) {
	for _, config := range []Config{{MaxReferences: -1}, {MaxReferenceBytes: -1}, {RetainedEpochs: -1}} {
		if _, err := NewCollector(&scriptedCaller{}, config); err == nil {
			t.Fatalf("negative retention admitted: %+v", config)
		}
	}
	collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if collector.config.MaxReferences != 4096 || collector.config.MaxReferenceBytes != 4*1024*1024 || collector.config.RetainedEpochs != 2 {
		t.Fatalf("zero/default retention differs: %+v", collector.config)
	}
	collector.nextRef = math.MaxUint64
	if _, err := collector.Capture(t.Context(), ModeFull, ""); err == nil || !strings.Contains(err.Error(), "identity exhausted") {
		t.Fatalf("reference counter wrapped: %v", err)
	}
	if len(collector.refs) != 0 || len(collector.identities) != 0 {
		t.Fatal("exhausted reference aliased a live identity")
	}
	collector.epoch, collector.root = math.MaxUint64, -1
	if _, err := collector.Capture(t.Context(), ModeFull, ""); err == nil || !strings.Contains(err.Error(), "epoch exhausted") {
		t.Fatalf("epoch counter wrapped: %v", err)
	}
}

func TestConcurrentCaptureAndResolutionPreserveRetentionOwnership(t *testing.T) {
	collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	first, err := collector.Capture(t.Context(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	ref := findBackend(t, first.Nodes, 4).Ref
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 20 {
				if _, err := collector.Capture(t.Context(), ModeFull, ""); err != nil {
					t.Errorf("concurrent capture: %v", err)
					return
				}
				result, err := collector.Resolve(t.Context(), ref)
				if err != nil || result.Status != ResolutionExact || result.Node == nil || result.Node.Ref != ref {
					t.Errorf("concurrent resolution: %+v error=%v", result, err)
					return
				}
				last := collector.LastSnapshot()
				if last.Schema != Schema {
					t.Errorf("incomplete concurrent snapshot: %+v", last)
					return
				}
			}
		}()
	}
	workers.Wait()
	assertReferenceBounds(t, collector)
}
