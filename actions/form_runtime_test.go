package actions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type recordingFormBackend struct {
	mu             sync.Mutex
	prefetches     int
	fills          int
	releases       int
	staleOnce      bool
	prefetchGate   chan struct{}
	prefetchResult FormPrefetchResult
	prefetchErr    error
}

func (b *recordingFormBackend) Prefetch(ctx context.Context, request FormPrefetchRequest) (FormPrefetchResult, error) {
	b.mu.Lock()
	b.prefetches++
	gate := b.prefetchGate
	configured := b.prefetchResult
	configuredErr := b.prefetchErr
	b.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return FormPrefetchResult{ResourceGroup: "cancelled", MutationToken: "cancelled"}, ctx.Err()
		}
	}
	if configured.ResourceGroup != "" || configured.MutationToken != "" || len(configured.Fields) != 0 || configuredErr != nil {
		return configured, configuredErr
	}
	result := FormPrefetchResult{
		ResourceGroup: fmt.Sprintf("group-%d", request.Generation),
		MutationToken: fmt.Sprintf("mutation-%d", request.Generation),
		Fields:        make([]PrefetchedFormField, len(request.Fields)),
	}
	for index, field := range request.Fields {
		result.Fields[index] = PrefetchedFormField{
			Name: field.Name, Selector: field.Selector, Ref: fmt.Sprintf("ref-%d-%s", request.Generation, field.Selector), Epoch: request.Generation,
		}
	}
	return result, nil
}

func (b *recordingFormBackend) Fill(_ context.Context, _ FormFillRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fills++
	if b.staleOnce {
		b.staleOnce = false
		return ErrFormIntentStale
	}
	return nil
}

func (b *recordingFormBackend) Release(_ context.Context, _ FormPrefetchResult) error {
	b.mu.Lock()
	b.releases++
	b.mu.Unlock()
	return nil
}

func (b *recordingFormBackend) counts() (prefetches, fills, releases int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.prefetches, b.fills, b.releases
}

func testFormIntent(values ...string) FormIntent {
	fields := make([]FormField, len(values))
	for index, value := range values {
		fields[index] = FormField{Name: fmt.Sprintf("field-%d", index), Selector: fmt.Sprintf("#field-%d", index), Value: value}
	}
	return FormIntent{SessionID: "session", PageID: "page", FormRoot: "#form", Fields: fields}
}

func TestFormIntentRuntimePrefetchesOnceAndReusesValueFreeCache(t *testing.T) {
	backend := &recordingFormBackend{}
	var events []FormIntentMetricEvent
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{MetricSink: func(event FormIntentMetricEvent) {
		events = append(events, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	intent := testFormIntent("alpha", "bravo")
	result, err := runtime.Execute(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	intent.Fields[0].Value = "changed-secret"
	intent.Fields[1].Value = ""
	second, err := runtime.Execute(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if result.FieldsFilled != 2 || second.FieldsFilled != 2 || result.Generation != second.Generation {
		t.Fatalf("unexpected results: first=%+v second=%+v", result, second)
	}
	prefetches, fills, releases := backend.counts()
	if prefetches != 1 || fills != 4 || releases != 0 {
		t.Fatalf("counts = prefetches:%d fills:%d releases:%d", prefetches, fills, releases)
	}
	metrics := runtime.Metrics()
	if metrics.FieldsPrefetchedTotal != 2 || metrics.CacheHitsTotal != 4 || metrics.MultiFieldFormsTotal != 2 {
		t.Fatalf("metrics = %+v", metrics)
	}
	emitted := make(map[FormIntentMetric]int)
	for _, event := range events {
		emitted[event.Name]++
	}
	for _, name := range []FormIntentMetric{MetricFormFieldsPrefetched, MetricFormCacheHits, MetricFormMultiField, MetricFormDuration} {
		if emitted[name] == 0 {
			t.Fatalf("metric %q not emitted: %v", name, emitted)
		}
	}
}

func TestFormIntentRuntimeInvalidationRefetchesAndStaleFillRetries(t *testing.T) {
	backend := &recordingFormBackend{staleOnce: true}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	intent := testFormIntent("value")
	result, err := runtime.Execute(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if result.Generation != 2 {
		t.Fatalf("generation = %d, want 2 after stale retry", result.Generation)
	}
	if err := runtime.Invalidate(intent.Identity()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	prefetches, fills, releases := backend.counts()
	if prefetches != 3 || fills != 3 || releases != 2 {
		t.Fatalf("counts = prefetches:%d fills:%d releases:%d", prefetches, fills, releases)
	}
	if runtime.Metrics().CacheInvalidationsTotal != 2 {
		t.Fatalf("invalidations = %d, want 2", runtime.Metrics().CacheInvalidationsTotal)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormIntentRuntimeConcurrentPrefetchIsDeduplicated(t *testing.T) {
	gate := make(chan struct{})
	backend := &recordingFormBackend{prefetchGate: gate}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	intent := testFormIntent("value")
	errorsByCall := make(chan error, 2)
	for range 2 {
		go func() {
			_, executeErr := runtime.Execute(context.Background(), intent)
			errorsByCall <- executeErr
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		prefetches, _, _ := backend.counts()
		if prefetches == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("prefetch did not start")
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)
	for range 2 {
		if err := <-errorsByCall; err != nil {
			t.Fatal(err)
		}
	}
	prefetches, fills, _ := backend.counts()
	if prefetches != 1 || fills != 2 {
		t.Fatalf("counts = prefetches:%d fills:%d", prefetches, fills)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormIntentRuntimeRefetchesBeforeExpandedBatchFill(t *testing.T) {
	backend := &recordingFormBackend{}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), testFormIntent("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), testFormIntent("two", "three")); err != nil {
		t.Fatal(err)
	}
	prefetches, fills, releases := backend.counts()
	if prefetches != 2 || fills != 3 || releases != 1 {
		t.Fatalf("counts = prefetches:%d fills:%d releases:%d", prefetches, fills, releases)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormIntentRuntimeCancellationReleasesPartialPrefetch(t *testing.T) {
	gate := make(chan struct{})
	backend := &recordingFormBackend{prefetchGate: gate}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runtime.Execute(ctx, testFormIntent("value"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	prefetches, fills, releases := backend.counts()
	if prefetches != 1 || fills != 0 || releases != 1 {
		t.Fatalf("counts = prefetches:%d fills:%d releases:%d", prefetches, fills, releases)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormIntentRuntimeReleasesMalformedTokenOnlyPrefetch(t *testing.T) {
	backend := &recordingFormBackend{prefetchResult: FormPrefetchResult{MutationToken: "orphan-observer"}}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), testFormIntent("value")); err == nil {
		t.Fatal("malformed prefetch accepted")
	}
	_, fills, releases := backend.counts()
	if fills != 0 || releases != 1 {
		t.Fatalf("fills=%d releases=%d, want 0 and 1", fills, releases)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormIntentRuntimeRejectsInvalidAndClosedExecution(t *testing.T) {
	backend := &recordingFormBackend{}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := testFormIntent("value")
	invalid.Fields = append(invalid.Fields, invalid.Fields[0])
	if _, err := runtime.Execute(context.Background(), invalid); err == nil {
		t.Fatal("duplicate selector accepted")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), testFormIntent("value")); !errors.Is(err, ErrFormIntentClosed) {
		t.Fatalf("error = %v, want closed", err)
	}
}

func BenchmarkFormIntentRuntimeCachedFiveField(b *testing.B) {
	backend := &recordingFormBackend{}
	runtime, err := NewFormIntentRuntime(backend, FormIntentRuntimeConfig{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			b.Errorf("close runtime: %v", err)
		}
	})
	intent := testFormIntent("one", "two", "three", "four", "five")
	if _, err := runtime.Execute(context.Background(), intent); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := runtime.Execute(context.Background(), intent); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	prefetches, _, _ := backend.counts()
	if prefetches != 1 {
		b.Fatalf("prefetches = %d, want 1", prefetches)
	}
}
