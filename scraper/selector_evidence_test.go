package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

func selectorTestDocument(t *testing.T, markup, pageURL string) *webapi.Document {
	t.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(markup), pageURL)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func verifiedDocumentSelector(doc *webapi.Document) SelectorVerifier {
	return func(ctx context.Context, selector string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return defaultValidateSelector(doc, selector), nil
	}
}

func TestStage2UnverifiedProposalsCannotPopulateVerifiedCache(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	defer closeScraperTestResource(t, "cache", cache.Close)
	hub := &mockInferenceHubLLM{responses: []InferenceHubLLMResponse{{Selector: "#absent", Confidence: .99}}}
	finder := NewAIFinderStage2(hub, &mockPrivacyRouter{}, cache, DefaultAIFinderStage2Config())
	result, err := finder.FindStage2(t.Context(), "example.test", "/", "https://example.test/", "target", `<button id="actual">Target</button>`)
	if err != nil || result.Verified || result.FromCache || result.Selector != "#absent" {
		t.Fatalf("proposal claimed browser verification: %+v err=%v", result, err)
	}
	pattern, err := selectorCachePattern("example.test", "/", "https://example.test/", "target")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := cache.Get("example.test", pattern); exists {
		t.Fatal("model confidence alone populated verified cache")
	}
}

func TestStage2VerifiedRejectsDuplicatesAndRepairsActualCacheDrift(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	defer closeScraperTestResource(t, "cache", cache.Close)
	before := selectorTestDocument(t, `<button id="old">Save</button>`, "https://example.test/")
	hub := &mockInferenceHubLLM{responses: []InferenceHubLLMResponse{{Selector: "#old", Confidence: .95}, {Selector: ".duplicate", Confidence: .99}, {Selector: "#new", Confidence: .9}}}
	finder := NewAIFinderStage2(hub, &mockPrivacyRouter{}, cache, DefaultAIFinderStage2Config())
	first, err := finder.FindStage2Verified(t.Context(), "example.test", "/", before.URL(), "save button", `<button id="old">Save</button>`, verifiedDocumentSelector(before))
	if err != nil || !first.Verified || first.Selector != "#old" {
		t.Fatalf("original actual selector failed: %+v err=%v", first, err)
	}
	after := selectorTestDocument(t, `<button id="new">Save</button><button class="duplicate">Save</button><button class="duplicate">Save</button>`, before.URL())
	second, err := finder.FindStage2Verified(t.Context(), "example.test", "/", after.URL(), "save button", `<button id="new">Save</button>`, verifiedDocumentSelector(after))
	if err != nil || !second.Verified || second.FromCache || second.Selector != "#new" || second.AttemptsUsed != 2 || hub.CallCount() != 3 {
		t.Fatalf("stale/ambiguous evidence admitted without bounded repair: %+v calls=%d err=%v", second, hub.CallCount(), err)
	}
	third, err := finder.FindStage2Verified(t.Context(), "example.test", "/", after.URL(), "save button", `<button id="new">Save</button>`, verifiedDocumentSelector(after))
	if err != nil || !third.Verified || !third.FromCache || hub.CallCount() != 3 {
		t.Fatalf("repaired current cache did not avoid inference: %+v err=%v", third, err)
	}
}

func TestStage2VerifiedPrivacyCancellationAndModeGuards(t *testing.T) {
	doc := selectorTestDocument(t, `<button id="target">Save</button>`, "https://example.test/")
	hub := &mockInferenceHubLLM{responses: []InferenceHubLLMResponse{{Selector: "#target", Confidence: .9, Local: false}}}
	finder := NewAIFinderStage2(hub, nil, nil, DefaultAIFinderStage2Config())
	if result, err := finder.FindStage2Verified(t.Context(), "example.test", "/", doc.URL(), "save", `<button id="target">Save</button>`, verifiedDocumentSelector(doc)); err == nil || result.Verified || hub.CallCount() != 1 {
		t.Fatalf("missing privacy router permitted an external response: %+v calls=%d err=%v", result, hub.CallCount(), err)
	}
	if _, err := finder.FindStage2Verified(t.Context(), "example.test", "/", doc.URL(), "save", "content", nil); err == nil {
		t.Fatal("nil verifier bypassed evidence admission")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := finder.FindStage2Verified(ctx, "example.test", "/", doc.URL(), "save", "content", verifiedDocumentSelector(doc)); err != context.Canceled || hub.CallCount() != 1 {
		t.Fatalf("cancelled request invoked inference: calls=%d err=%v", hub.CallCount(), err)
	}
	finder.SetMode("invalid")
	if _, err := finder.FindStage2(t.Context(), "example.test", "/", doc.URL(), "save", "content"); err == nil || hub.CallCount() != 1 {
		t.Fatalf("unsupported mode reached inference: calls=%d err=%v", hub.CallCount(), err)
	}
}

func TestAdaptiveCacheFailedPersistenceCannotPublishMemoryEntry(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cache.PutContext(t.Context(), AdaptiveEntry{Domain: "example.test", URLPattern: "/", Selector: "#target", Confidence: .9}); err == nil || len(cache.l1) != 0 {
		t.Fatalf("failed SQLite write fabricated an L1 success: entries=%d err=%v", len(cache.l1), err)
	}
	if _, exists, err := cache.LookupContext(t.Context(), "example.test", "/"); err == nil || exists {
		t.Fatalf("failed SQLite lookup concealed as an actual cache result: exists=%v err=%v", exists, err)
	}
	doc := selectorTestDocument(t, `<button id="target">Save</button>`, "https://example.test/")
	calls := 0
	finder := NewAIFinder(cache, func(context.Context, string, string) (string, error) { calls++; return "#target", nil })
	if _, err := finder.Find(t.Context(), doc, "example.test", "/", "save"); err == nil || calls != 0 {
		t.Fatalf("failed persistence caused blind model retry: calls=%d err=%v", calls, err)
	}
}

func TestStage2VerifierCannotPublishAfterCancellation(t *testing.T) {
	hub := &mockInferenceHubLLM{responses: []InferenceHubLLMResponse{{Selector: "#target", Confidence: .9}}}
	finder := NewAIFinderStage2(hub, &mockPrivacyRouter{}, nil, DefaultAIFinderStage2Config())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := finder.FindStage2Verified(ctx, "example.test", "/", "https://example.test/", "save", `<button id="target">Save</button>`, func(context.Context, string) (bool, error) { cancel(); return true, nil })
	if err != context.Canceled || result.Verified || result.Selector != "" {
		t.Fatalf("expired verification became successful evidence: %+v err=%v", result, err)
	}
}

func TestStage2ConcurrentModeChangesKeepOperationModeConsistent(t *testing.T) {
	hub := &mockInferenceHubLLM{responses: make([]InferenceHubLLMResponse, 40)}
	for i := range hub.responses {
		hub.responses[i] = InferenceHubLLMResponse{Selector: "#target", Coordinates: "20,30", Confidence: .9}
	}
	finder := NewAIFinderStage2(hub, &mockPrivacyRouter{}, nil, DefaultAIFinderStage2Config())
	var joined sync.WaitGroup
	joined.Add(1)
	stop := make(chan struct{})
	go func() {
		defer joined.Done()
		for {
			select {
			case <-stop:
				return
			default:
				finder.SetMode(FinderModeText)
				finder.SetMode(FinderModeVision)
			}
		}
	}()
	defer func() { close(stop); joined.Wait() }()
	for range 40 {
		result, err := finder.FindStage2(t.Context(), "example.test", "/", "https://example.test/", "save", "bounded evidence")
		if err != nil {
			t.Fatal(err)
		}
		req := hub.LastRequest()
		if result.Mode != req.Mode || result.Mode == FinderModeText && result.Selector != "#target" || result.Mode == FinderModeVision && result.Selector != "20,30" {
			t.Fatalf("one operation mixed mutable modes: result=%+v request=%+v", result, req)
		}
	}
}

func TestAIFinderUsesActualRedactedDOMAndUniqueSelectors(t *testing.T) {
	doc := selectorTestDocument(t, `<title>Generic title</title><button id="login">Sign in</button><input type="password" value="credential-value-123"><textarea>private-note-value</textarea><script>private-script-value</script>`, "https://example.test/login")
	calls := 0
	finder := NewAIFinder(nil, func(_ context.Context, evidence, intent string) (string, error) {
		calls++
		var observed selectorDocumentEvidence
		if err := json.Unmarshal([]byte(evidence), &observed); err != nil {
			return "", err
		}
		if observed.Schema != "artemis.selector-context.v1" || len(evidence) > selectorEvidenceBytes || intent != "login button" {
			t.Fatalf("invalid actual selector context: %s", evidence)
		}
		found := false
		for _, node := range observed.Nodes {
			found = found || node.Tag == "button" && node.Attributes["id"] == "login" && node.Text == "Sign in"
		}
		if !found || strings.Contains(evidence, "credential-value-123") || strings.Contains(evidence, "private-note-value") || strings.Contains(evidence, "private-script-value") {
			t.Fatalf("context omitted DOM target or leaked excluded content: %s", evidence)
		}
		return "#login", nil
	})
	finder.Validate = nil
	selector, err := finder.Find(t.Context(), doc, "example.test", "/login", "login button")
	if err != nil || selector != "#login" || calls != 1 {
		t.Fatalf("actual unique target failed: selector=%q calls=%d err=%v", selector, calls, err)
	}
	duplicate := selectorTestDocument(t, `<button class="same">Sign in</button><button class="same">Sign in</button>`, doc.URL())
	for _, selector := range []string{".same", "missing", "//button[@id='absent']", "Sign in"} {
		if defaultValidateSelector(duplicate, selector) {
			t.Fatalf("ambiguous/missing/heuristic proposal admitted: %q", selector)
		}
	}
	finder.Proposer = func(context.Context, string, string) (string, error) { calls++; return ".same", nil }
	finder.Validate = func(*webapi.Document, string) bool { return true }
	finder.MaxTry = 100
	if _, err := finder.Find(t.Context(), duplicate, "example.test", "/login", "login button"); err == nil || calls != 4 {
		t.Fatalf("custom validation bypassed uniqueness or attempt cap: calls=%d err=%v", calls, err)
	}
}

func TestAIFinderInvalidatesStalePersistentCacheAndBindsIntentOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adaptive.db")
	cache, err := OpenAdaptiveCache(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer closeScraperTestResource(t, "cache", cache.Close)
	doc := selectorTestDocument(t, `<button id="old">Sign in</button><button id="other">Search</button>`, "https://example.test/login")
	calls := 0
	finder := NewAIFinder(cache, func(context.Context, string, string) (string, error) { calls++; return "#old", nil })
	for range 2 {
		if selector, err := finder.Find(t.Context(), doc, "example.test", "/login", "login button"); err != nil || selector != "#old" {
			t.Fatalf("actual warmed selector failed: %q %v", selector, err)
		}
	}
	if calls != 1 {
		t.Fatalf("validated live cache did not avoid repeated inference: %d", calls)
	}
	pattern, err := selectorCachePattern("example.test", "/login", doc.URL(), "login button")
	if err != nil {
		t.Fatal(err)
	}
	finder.Proposer = func(context.Context, string, string) (string, error) {
		calls++
		return "", errors.New("provider unavailable")
	}
	changed := selectorTestDocument(t, `<button id="renamed">Sign in</button>`, doc.URL())
	if selector, err := finder.Find(t.Context(), changed, "example.test", "/login", "login button"); err == nil || selector != "" {
		t.Fatalf("stale DOM selector admitted: %q %v", selector, err)
	}
	if _, exists := cache.Get("example.test", pattern); exists {
		t.Fatal("stale selector remained in memory")
	}
	reopened, err := OpenAdaptiveCache(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer closeScraperTestResource(t, "reopened", reopened.Close)
	if _, exists := reopened.Get("example.test", pattern); exists {
		t.Fatal("stale selector remained in SQLite")
	}
	for _, test := range []struct{ intent, pageURL string }{
		{"search button", doc.URL()},
		{"login button", "http://example.test/login"},
		{"login button", "https://example.test:444/login"},
	} {
		other, err := selectorCachePattern("example.test", "/login", test.pageURL, test.intent)
		if err != nil || other == pattern {
			t.Fatalf("different intent/origin shared its cache: %+v err=%v", test, err)
		}
	}
	if _, err := selectorCachePattern("foreign.test", "/login", doc.URL(), "login button"); err == nil {
		t.Fatal("foreign cache domain admitted")
	}
}

func TestSelectorEvidenceIsBoundedAndCancellationPreventsInference(t *testing.T) {
	markup := `<div>` + strings.Repeat(`<button class="`+strings.Repeat("Ä", 200)+`">Continue</button>`, 1000) + `</div>`
	doc := selectorTestDocument(t, markup, "https://example.test/")
	raw, err := selectorPageEvidence(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	var evidence selectorDocumentEvidence
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil || !evidence.Truncated || len(raw) > selectorEvidenceBytes || len(evidence.Nodes) > selectorEvidenceNodes {
		t.Fatalf("unbounded/invalid UTF-8 context: bytes=%d nodes=%d truncated=%v err=%v", len(raw), len(evidence.Nodes), evidence.Truncated, err)
	}
	calls := 0
	finder := NewAIFinder(nil, func(context.Context, string, string) (string, error) { calls++; return "button", nil })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := finder.Find(ctx, doc, "example.test", "/", "continue button"); err != context.Canceled || calls != 0 {
		t.Fatalf("cancelled request invoked inference: calls=%d err=%v", calls, err)
	}
}

func TestSelectorEvidenceTextClippingIsExplicit(t *testing.T) {
	doc := selectorTestDocument(t, `<button title="`+strings.Repeat("Ä", 200)+`">`+strings.Repeat("Save ", 100)+`</button>`, "https://example.test/")
	raw, err := selectorPageEvidence(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	var evidence selectorDocumentEvidence
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil || !evidence.Truncated || len(evidence.Nodes) >= selectorEvidenceNodes {
		t.Fatalf("field clipping concealed as complete DOM context: truncated=%v nodes=%d err=%v", evidence.Truncated, len(evidence.Nodes), err)
	}
}
