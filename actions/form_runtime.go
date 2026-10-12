package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const defaultFormIntentCacheSize = 128

var (
	ErrFormIntentClosed = errors.New("form intent runtime closed")
	ErrFormIntentStale  = errors.New("form intent cache stale")
)

type FormFieldDescriptor struct {
	Name     string
	Selector string
}

type FormBoxModel struct {
	Content [8]float64
	Padding [8]float64
	Border  [8]float64
	Margin  [8]float64
	Width   int
	Height  int
}

type PrefetchedFormField struct {
	Name     string
	Selector string
	Ref      string
	Box      FormBoxModel
	Epoch    uint64
}

type FormPrefetchRequest struct {
	Identity   FormIdentity
	Generation uint64
	Fields     []FormFieldDescriptor
}

type FormPrefetchResult struct {
	ResourceGroup string
	MutationToken string
	Fields        []PrefetchedFormField
}

type FormFillRequest struct {
	Identity   FormIdentity
	Generation uint64
	Field      PrefetchedFormField
	Value      string
}

type FormIntentBackend interface {
	Prefetch(context.Context, FormPrefetchRequest) (FormPrefetchResult, error)
	Fill(context.Context, FormFillRequest) error
	Release(context.Context, FormPrefetchResult) error
}

type FormIntentMetric string

const (
	MetricFormFieldsPrefetched FormIntentMetric = "artemis_form_intent_fields_prefetched_total"
	MetricFormCacheHits        FormIntentMetric = "artemis_form_intent_cache_hits_total"
	MetricFormInvalidations    FormIntentMetric = "artemis_form_intent_cache_invalidations_total"
	MetricFormMultiField       FormIntentMetric = "artemis_form_intent_multi_field_forms_total"
	MetricFormDuration         FormIntentMetric = "artemis_form_intent_duration_ms"
)

type FormIntentMetricEvent struct {
	Name  FormIntentMetric
	Value int64
}

type FormIntentRuntimeConfig struct {
	MaxForms   int
	MetricSink func(FormIntentMetricEvent)
}

type FormIntentResult struct {
	FieldsFilled int
	Generation   uint64
}

type FormIntentMetrics struct {
	FieldsPrefetchedTotal   uint64
	CacheHitsTotal          uint64
	CacheInvalidationsTotal uint64
	MultiFieldFormsTotal    uint64
	DurationBuckets         [7]uint64
}

type formIntentMetricState struct {
	fieldsPrefetched atomic.Uint64
	cacheHits        atomic.Uint64
	invalidations    atomic.Uint64
	multiField       atomic.Uint64
	durations        [7]atomic.Uint64
}

type formCacheEntry struct {
	generation uint64
	result     FormPrefetchResult
	fields     map[string]PrefetchedFormField
	used       uint64
}

type formPrefetchCall struct{ done chan struct{} }

type FormIntentRuntime struct {
	backend        FormIntentBackend
	metricSink     func(FormIntentMetricEvent)
	maxForms       int
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	cache          map[FormIdentity]*formCacheEntry
	generations    map[FormIdentity]uint64
	inflight       map[FormIdentity]*formPrefetchCall
	sequence       uint64
	nextGeneration uint64
	closed         bool
	metrics        formIntentMetricState
}

func NewFormIntentRuntime(backend FormIntentBackend, config FormIntentRuntimeConfig) (*FormIntentRuntime, error) {
	if backend == nil {
		return nil, errors.New("form intent runtime: backend required")
	}
	if config.MaxForms < 0 {
		return nil, errors.New("form intent runtime: max forms must not be negative")
	}
	if config.MaxForms == 0 {
		config.MaxForms = defaultFormIntentCacheSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &FormIntentRuntime{
		backend: backend, metricSink: config.MetricSink, maxForms: config.MaxForms, ctx: ctx, cancel: cancel,
		cache: make(map[FormIdentity]*formCacheEntry), generations: make(map[FormIdentity]uint64), inflight: make(map[FormIdentity]*formPrefetchCall),
	}, nil
}

func (r *FormIntentRuntime) Execute(ctx context.Context, intent FormIntent) (result FormIntentResult, resultErr error) {
	if ctx == nil {
		return result, errors.New("form intent execute: context required")
	}
	if err := intent.Validate(); err != nil {
		return result, err
	}
	opCtx, release := r.operationContext(ctx)
	defer release()
	started := time.Now()
	defer func() { r.recordDuration(time.Since(started)) }()
	if len(intent.Fields) > 1 {
		r.recordMetric(MetricFormMultiField, 1)
	}
	if err := r.ensureIntentCache(opCtx, intent); err != nil {
		return result, err
	}
	for _, field := range intent.Fields {
		generation, err := r.executeField(opCtx, intent, field)
		if err != nil {
			return result, err
		}
		result.FieldsFilled++
		result.Generation = generation
	}
	return result, nil
}

func (r *FormIntentRuntime) executeField(ctx context.Context, intent FormIntent, field FormField) (uint64, error) {
	identity := intent.Identity()
	for attempt := 0; attempt < 2; attempt++ {
		generation, err := r.fillCached(ctx, identity, field)
		if !errors.Is(err, ErrFormIntentStale) {
			return generation, err
		}
		if invalidateErr := r.Invalidate(identity); invalidateErr != nil {
			return 0, errors.Join(err, invalidateErr)
		}
		if attempt == 1 {
			return 0, ErrFormIntentStale
		}
		if err := r.ensureIntentCache(ctx, intent); err != nil {
			return 0, err
		}
	}
	return 0, ErrFormIntentStale
}

func (r *FormIntentRuntime) ensureIntentCache(ctx context.Context, intent FormIntent) error {
	identity := intent.Identity()
	r.mu.RLock()
	entry := r.cache[identity]
	compatible := entry == nil || entry.generation == r.generations[identity] && formCacheSupports(entry, intent.Fields)
	r.mu.RUnlock()
	if !compatible {
		if err := r.Invalidate(identity); err != nil {
			return err
		}
	}
	return r.ensureCache(ctx, intent)
}

func (r *FormIntentRuntime) fillCached(ctx context.Context, identity FormIdentity, field FormField) (uint64, error) {
	r.mu.RLock()
	entry := r.cache[identity]
	if r.closed {
		r.mu.RUnlock()
		return 0, ErrFormIntentClosed
	}
	if entry == nil || entry.generation != r.generations[identity] {
		r.mu.RUnlock()
		return 0, ErrFormIntentStale
	}
	prefetched, ok := entry.fields[field.Selector]
	if !ok || prefetched.Name != field.Name {
		r.mu.RUnlock()
		return 0, ErrFormIntentStale
	}
	err := r.backend.Fill(ctx, FormFillRequest{Identity: identity, Generation: entry.generation, Field: prefetched, Value: field.Value})
	generation := entry.generation
	r.mu.RUnlock()
	if err != nil {
		if errors.Is(err, ErrFormIntentStale) {
			return 0, err
		}
		if invalidateErr := r.Invalidate(identity); invalidateErr != nil {
			return 0, errors.Join(err, invalidateErr)
		}
		return 0, err
	}
	r.recordMetric(MetricFormCacheHits, 1)
	return generation, nil
}

func (r *FormIntentRuntime) ensureCache(ctx context.Context, intent FormIntent) error {
	identity := intent.Identity()
	for {
		request, wait, ready, err := r.reservePrefetch(identity, intent.Fields)
		if err != nil || ready {
			return err
		}
		if wait != nil {
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return r.runPrefetch(ctx, request)
	}
}

func (r *FormIntentRuntime) reservePrefetch(identity FormIdentity, fields []FormField) (FormPrefetchRequest, <-chan struct{}, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return FormPrefetchRequest{}, nil, false, ErrFormIntentClosed
	}
	if entry := r.cache[identity]; entry != nil && entry.generation == r.generations[identity] {
		r.touchLocked(entry)
		return FormPrefetchRequest{}, nil, true, nil
	}
	if call := r.inflight[identity]; call != nil {
		return FormPrefetchRequest{}, call.done, false, nil
	}
	if r.nextGeneration == math.MaxUint64 {
		return FormPrefetchRequest{}, nil, false, errors.New("form intent generation exhausted")
	}
	r.nextGeneration++
	r.generations[identity] = r.nextGeneration
	r.inflight[identity] = &formPrefetchCall{done: make(chan struct{})}
	return FormPrefetchRequest{Identity: identity, Generation: r.nextGeneration, Fields: describeFormFields(fields)}, nil, false, nil
}

func (r *FormIntentRuntime) runPrefetch(ctx context.Context, request FormPrefetchRequest) error {
	result, err := r.backend.Prefetch(ctx, request)
	if err == nil {
		err = validatePrefetchResult(request, result)
	}
	var evicted *formCacheEntry
	r.mu.Lock()
	call := r.inflight[request.Identity]
	delete(r.inflight, request.Identity)
	if err == nil && !r.closed && r.generations[request.Identity] == request.Generation {
		entry := newFormCacheEntry(request.Generation, result)
		r.touchLocked(entry)
		r.cache[request.Identity] = entry
		evicted = r.evictLocked(request.Identity)
	} else if err == nil {
		err = ErrFormIntentStale
	}
	if err != nil && r.cache[request.Identity] == nil && r.inflight[request.Identity] == nil {
		delete(r.generations, request.Identity)
	}
	if call != nil {
		close(call.done)
	}
	r.mu.Unlock()
	if err != nil {
		return errors.Join(err, r.releaseResult(result))
	}
	if evicted != nil {
		if releaseErr := r.releaseResult(evicted.result); releaseErr != nil {
			return errors.Join(releaseErr, r.Invalidate(request.Identity))
		}
	}
	r.recordMetric(MetricFormFieldsPrefetched, int64(len(result.Fields)))
	return nil
}

func (r *FormIntentRuntime) Invalidate(identity FormIdentity) error {
	r.mu.Lock()
	entry := r.cache[identity]
	_, pending := r.inflight[identity]
	delete(r.cache, identity)
	var generationErr error
	if pending {
		if r.nextGeneration == math.MaxUint64 {
			r.generations[identity] = 0
			generationErr = errors.New("form intent generation exhausted")
		} else {
			r.nextGeneration++
			r.generations[identity] = r.nextGeneration
		}
	} else {
		delete(r.generations, identity)
	}
	r.mu.Unlock()
	if entry != nil || pending {
		r.recordMetric(MetricFormInvalidations, 1)
	}
	if entry == nil {
		return generationErr
	}
	releaseErr := r.releaseResult(entry.result)
	return errors.Join(generationErr, releaseErr)
}

func (r *FormIntentRuntime) InvalidatePage(sessionID, pageID string) error {
	r.mu.RLock()
	identitySet := make(map[FormIdentity]struct{})
	for identity := range r.cache {
		if identity.SessionID == sessionID && identity.PageID == pageID {
			identitySet[identity] = struct{}{}
		}
	}
	for identity := range r.inflight {
		if identity.SessionID == sessionID && identity.PageID == pageID {
			identitySet[identity] = struct{}{}
		}
	}
	r.mu.RUnlock()
	var resultErr error
	for identity := range identitySet {
		resultErr = errors.Join(resultErr, r.Invalidate(identity))
	}
	return resultErr
}

func (r *FormIntentRuntime) Metrics() FormIntentMetrics {
	result := FormIntentMetrics{
		FieldsPrefetchedTotal: r.metrics.fieldsPrefetched.Load(), CacheHitsTotal: r.metrics.cacheHits.Load(),
		CacheInvalidationsTotal: r.metrics.invalidations.Load(), MultiFieldFormsTotal: r.metrics.multiField.Load(),
	}
	for index := range result.DurationBuckets {
		result.DurationBuckets[index] = r.metrics.durations[index].Load()
	}
	return result
}

func (r *FormIntentRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.cancel()
	waits := make([]<-chan struct{}, 0, len(r.inflight))
	for _, call := range r.inflight {
		waits = append(waits, call.done)
	}
	r.mu.Unlock()
	for _, wait := range waits {
		<-wait
	}
	results := r.detachResults()
	var resultErr error
	for _, result := range results {
		resultErr = errors.Join(resultErr, r.releaseResult(result))
	}
	if closer, ok := r.backend.(io.Closer); ok {
		resultErr = errors.Join(resultErr, closer.Close())
	}
	return resultErr
}

func (r *FormIntentRuntime) operationContext(ctx context.Context) (context.Context, func()) {
	opCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	return opCtx, func() {
		stop()
		cancel()
	}
}

func (r *FormIntentRuntime) detachResults() []FormPrefetchResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	results := make([]FormPrefetchResult, 0, len(r.cache))
	for _, entry := range r.cache {
		results = append(results, entry.result)
	}
	r.cache = make(map[FormIdentity]*formCacheEntry)
	r.generations = make(map[FormIdentity]uint64)
	return results
}

func (r *FormIntentRuntime) releaseResult(result FormPrefetchResult) error {
	if result.ResourceGroup == "" && result.MutationToken == "" && len(result.Fields) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.backend.Release(ctx, result)
}

func (r *FormIntentRuntime) touchLocked(entry *formCacheEntry) {
	r.sequence++
	entry.used = r.sequence
}

func (r *FormIntentRuntime) evictLocked(keep FormIdentity) *formCacheEntry {
	if len(r.cache) <= r.maxForms {
		return nil
	}
	var oldestID FormIdentity
	var oldest *formCacheEntry
	for identity, entry := range r.cache {
		if identity == keep || oldest != nil && entry.used >= oldest.used {
			continue
		}
		oldestID, oldest = identity, entry
	}
	if oldest != nil {
		delete(r.cache, oldestID)
		delete(r.generations, oldestID)
	}
	return oldest
}

func (r *FormIntentRuntime) recordMetric(name FormIntentMetric, value int64) {
	if value <= 0 {
		return
	}
	switch name {
	case MetricFormFieldsPrefetched:
		r.metrics.fieldsPrefetched.Add(uint64(value))
	case MetricFormCacheHits:
		r.metrics.cacheHits.Add(uint64(value))
	case MetricFormInvalidations:
		r.metrics.invalidations.Add(uint64(value))
	case MetricFormMultiField:
		r.metrics.multiField.Add(uint64(value))
	}
	if r.metricSink != nil {
		r.metricSink(FormIntentMetricEvent{Name: name, Value: value})
	}
}

func (r *FormIntentRuntime) recordDuration(duration time.Duration) {
	millis := duration.Milliseconds()
	bounds := [...]int64{10, 50, 100, 250, 500, 1000}
	bucket := len(bounds)
	for index, bound := range bounds {
		if millis <= bound {
			bucket = index
			break
		}
	}
	r.metrics.durations[bucket].Add(1)
	if r.metricSink != nil {
		r.metricSink(FormIntentMetricEvent{Name: MetricFormDuration, Value: millis})
	}
}

func describeFormFields(fields []FormField) []FormFieldDescriptor {
	result := make([]FormFieldDescriptor, len(fields))
	for index, field := range fields {
		result[index] = FormFieldDescriptor{Name: field.Name, Selector: field.Selector}
	}
	return result
}

func validatePrefetchResult(request FormPrefetchRequest, result FormPrefetchResult) error {
	if result.ResourceGroup == "" || result.MutationToken == "" {
		return errors.New("form intent prefetch: resource group and mutation token required")
	}
	if len(result.Fields) != len(request.Fields) {
		return fmt.Errorf("form intent prefetch: got %d fields, want %d", len(result.Fields), len(request.Fields))
	}
	wanted := make(map[string]string, len(request.Fields))
	for _, field := range request.Fields {
		wanted[field.Selector] = field.Name
	}
	for _, field := range result.Fields {
		name, ok := wanted[field.Selector]
		if !ok || name != field.Name || field.Ref == "" {
			return fmt.Errorf("form intent prefetch: invalid field %q", field.Selector)
		}
		delete(wanted, field.Selector)
	}
	if len(wanted) != 0 {
		return errors.New("form intent prefetch: missing fields")
	}
	return nil
}

func newFormCacheEntry(generation uint64, result FormPrefetchResult) *formCacheEntry {
	fields := make(map[string]PrefetchedFormField, len(result.Fields))
	for _, field := range result.Fields {
		fields[field.Selector] = field
	}
	return &formCacheEntry{generation: generation, result: result, fields: fields}
}

func formCacheSupports(entry *formCacheEntry, fields []FormField) bool {
	for _, field := range fields {
		prefetched, ok := entry.fields[field.Selector]
		if !ok || prefetched.Name != field.Name {
			return false
		}
	}
	return true
}
