package replay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func declarationForTest() fixtureasset.Manifest {
	return fixtureasset.Manifest{OwnerSubsystem: "browser-fixtures", FixtureKind: fixtureasset.KindReplay,
		PrivacyClass: fixtureasset.PrivacyInternal, ReplayMode: fixtureasset.ReplayIsolated,
		RunnerRef: RunnerRef, QualityGateRef: "fixture:semantic-regression", Retention: "fixture-evidence"}
}

func observationForTest() Observation {
	return Observation{ScenarioID: "snapshot", Runner: "renderless", URL: "https://example.invalid/snapshot", StatusCode: 200,
		Title: "Captured", Text: "actual captured text", HTML: "<html><head><title>Captured</title></head><body><p>actual captured text</p></body></html>"}
}

func capturedAssetForTest(t *testing.T, policy Policy) (fixtureasset.Manifest, []byte) {
	t.Helper()
	manifest, content, err := CaptureSnapshot(observationForTest(), declarationForTest(), policy)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, content
}

func TestReplayAssetUsesExistingCaptureProfileAndCompleteBinding(t *testing.T) {
	manifest, content := capturedAssetForTest(t, Policy{})
	if err := ValidateAsset(content, manifest, Policy{}); err != nil {
		t.Fatal(err)
	}
	unbound := manifest.Detached()
	unbound.AssetHash, unbound.AssetPath = "", ""
	bound, err := BindAsset(content, unbound, Policy{})
	if err != nil || bound.AssetHash != manifest.AssetHash || bound.AssetPath != manifest.AssetPath || bound.ID != manifest.ID {
		t.Fatalf("existing captured record binding changed identity: %+v %v", bound, err)
	}
	for _, name := range []string{"id", "kind", "runner", "mode", "hash", "path", "bytes"} {
		t.Run(name, func(t *testing.T) {
			changed, data := manifest.Detached(), bytes.Clone(content)
			switch name {
			case "id":
				changed.ID = "other"
			case "kind":
				changed.FixtureKind = fixtureasset.KindBenchmark
			case "runner":
				changed.RunnerRef = "other"
			case "mode":
				changed.ReplayMode = fixtureasset.ReplayShadow
			case "hash":
				changed.AssetHash = strings.Repeat("0", 64)
			case "path":
				changed.AssetPath = "/other"
			case "bytes":
				data = bytes.Replace(data, []byte("actual captured text"), []byte("altered captured text"), 1)
				changed.AssetHash = fmt.Sprintf("%x", sha256.Sum256(data))
				changed.AssetPath = "content://sha256/" + changed.AssetHash
			}
			if err := ValidateAsset(data, changed, Policy{}); err == nil {
				t.Fatal("changed input acquired replay admission")
			}
		})
	}
	for _, policy := range []Policy{{AllowUnredacted: true}, {Patterns: []string{"unmatched-custom-profile"}}, {Patterns: []string{"["}}} {
		if err := ValidateAsset(content, manifest, policy); err == nil {
			t.Fatal("different or invalid profile admitted an existing capture")
		}
	}
	raw, rawContent := capturedAssetForTest(t, Policy{AllowUnredacted: true})
	if err := ValidateAsset(rawContent, raw, Policy{}); err == nil {
		t.Fatal("unredacted record admitted by default")
	}
	if err := ValidateAsset(rawContent, raw, Policy{AllowUnredacted: true}); err != nil {
		t.Fatal(err)
	}
}

func TestReplaySnapshotRunsActualDocumentWithoutScriptOrExternalRequests(t *testing.T) {
	var requests atomic.Int64
	probe := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if _, err := io.WriteString(writer, "actual probe"); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(probe.Close)
	response, err := http.Get(probe.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("live probe control was not reached")
	}
	requests.Store(0)
	observed := observationForTest()
	observed.HTML = `<html><head><title>Captured</title></head><body><p>actual captured text</p><script>document.title='changed';fetch('` + probe.URL + `')</script><img src="` + probe.URL + `" onerror="document.title='changed'"></body></html>`
	manifest, content, err := CaptureSnapshot(observed, declarationForTest(), Policy{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(t.Context(), content, manifest, Policy{})
	if err != nil || !result.Passed || result.StatusCode != 200 || result.Title != observed.Title || !strings.Contains(result.Text, observed.Text) || result.AssetHash != manifest.AssetHash || requests.Load() != 0 {
		t.Fatalf("actual inert replay: %+v requests=%d err=%v", result, requests.Load(), err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := Run(cancelled, content, manifest, Policy{}); err == nil || result.Passed || result.CaptureID != "" {
		t.Fatal("cancelled owner reached replay execution")
	}
}

func TestReplaySnapshotSemanticFailureCannotBecomePassingEvidence(t *testing.T) {
	observed := observationForTest()
	observed.Title = "different observed title"
	manifest, content, err := CaptureSnapshot(observed, declarationForTest(), Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Run(t.Context(), content, manifest, Policy{}); err == nil || result.Passed || result.Title != "Captured" {
		t.Fatalf("actual mismatch became passing snapshot evidence: %+v %v", result, err)
	}
}

func TestReplaySnapshotPreservesActualPlaintextRepresentation(t *testing.T) {
	observed := observationForTest()
	observed.HTML, observed.Title = "", ""
	observed.Text = "literal <title>data title</title><script>data script</script> and <tag>data</tag>"
	manifest, content, err := CaptureSnapshot(observed, declarationForTest(), Policy{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(t.Context(), content, manifest, Policy{})
	if err != nil || !result.Passed || result.Title != "" || result.Text != observed.Text {
		t.Fatalf("literal text/plain snapshot interpreted as HTML: %+v %v", result, err)
	}
}

func TestReplaySnapshotCustomProfileRunsItsActualRedactedBytes(t *testing.T) {
	observed := observationForTest()
	observed.Title = "private-literal"
	observed.HTML = strings.ReplaceAll(observed.HTML, "Captured", observed.Title)
	policy := Policy{Patterns: []string{"private-literal"}, Mask: "[MASK]"}
	manifest, content, err := CaptureSnapshot(observed, declarationForTest(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte("private-literal")) {
		t.Fatal("custom-sensitive literal survived capture")
	}
	result, err := Run(t.Context(), content, manifest, policy)
	if err != nil || !result.Passed || result.Title != policy.Mask {
		t.Fatalf("actual custom-profile snapshot: %+v %v", result, err)
	}
	if result, err := Run(t.Context(), content, manifest, Policy{}); err == nil || result.Passed || result.CaptureID != "" {
		t.Fatal("default profile executed a different capture profile")
	}
}

func TestReplaySnapshotNetworkPolicyDeniesOtherOriginMethodAndScheme(t *testing.T) {
	var ownedHits, foreignHits atomic.Int64
	owned := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ownedHits.Add(1)
		if _, err := io.WriteString(writer, "<html><body>owned</body></html>"); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(owned.Close)
	foreign := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		foreignHits.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(foreign.Close)
	policy, err := snapshotNetworkPolicy(owned.URL)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := engine.New(engine.Config{PolicyConfig: policy, MaxBodyBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	page, err := runner.Fetch(t.Context(), owned.URL, engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, url, method string }{
		{"other port", foreign.URL, "GET"},
		{"other hostname", strings.Replace(owned.URL, "127.0.0.1", "localhost", 1), "GET"},
		{"other scheme", strings.Replace(owned.URL, "http://", "https://", 1), "GET"},
		{"mutating method", owned.URL, "POST"},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := runner.Fetch(t.Context(), test.url, engine.FetchOpts{Method: test.method})
			if page != nil {
				if closeErr := page.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if err == nil || page != nil || foreignHits.Load() != 0 || ownedHits.Load() != 1 {
				t.Fatalf("denied target reached a socket: owned=%d foreign=%d err=%v", ownedHits.Load(), foreignHits.Load(), err)
			}
		})
	}
}
