package js

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	v8 "github.com/tommie/v8go"
)

// NavigatorConfig configures the values returned by window.navigator.
// Zero values are replaced by Artemis defaults.
type NavigatorConfig struct {
	UserAgent string
	Language  string
	Languages []string
	Platform  string
	// Vendor defaults to "Google Inc." — the renderless path presents a
	// coherent Chrome identity, not a library.
	Vendor string
	// HardwareConcurrency/DeviceMemory mirror a typical desktop. Zero
	// selects the defaults (8 cores / 8 GB).
	HardwareConcurrency int
	DeviceMemory        float64
	// MaxTouchPoints matches desktop Chrome (0). pdfViewerEnabled is
	// handled JS-side (navigator extras bootstrap defaults it to true).
	MaxTouchPoints int
}

func (n *NavigatorConfig) applyDefaults() {
	if n.UserAgent == "" {
		n.UserAgent = "Mozilla/5.0 (Artemis/0.0.1) AppleWebKit/537.36"
	}
	if n.Language == "" {
		n.Language = "en-US"
	}
	if len(n.Languages) == 0 {
		n.Languages = []string{n.Language}
	}
	if n.Platform == "" {
		n.Platform = "Linux x86_64"
	}
	if n.Vendor == "" {
		n.Vendor = "Google Inc."
	}
	if n.HardwareConcurrency == 0 {
		n.HardwareConcurrency = 8
	}
	if n.DeviceMemory == 0 {
		n.DeviceMemory = 8
	}
}

// memStorage is an in-memory implementation of the Storage WebAPI.
type memStorage struct {
	mu     sync.Mutex
	keys   []string
	items  map[string]string
	bytes  int
	budget *storageBudget
}

func newMemStorage() *memStorage {
	return &memStorage{items: make(map[string]string)}
}

func (s *memStorage) getItem(k string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[k]
	return v, ok
}

func (s *memStorage) setItem(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.items[k]
	nextBytes := s.bytes + storageBytes(k, v)
	if exists {
		nextBytes -= storageBytes(k, old)
	}
	if nextBytes > MaxStorageBytes || (!exists && len(s.items) >= MaxStorageKeys) {
		return errors.New("web storage quota exceeded")
	}
	newKeys := 0
	if !exists {
		newKeys = 1
	}
	if err := s.budget.reserve(nextBytes-s.bytes, newKeys); err != nil {
		return err
	}
	if !exists {
		s.keys = append(s.keys, k)
	}
	s.items[k] = v
	s.bytes = nextBytes
	return nil
}

func (s *memStorage) removeItem(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.items[k]
	if !ok {
		return
	}
	s.bytes -= storageBytes(k, value)
	if err := s.budget.reserve(-storageBytes(k, value), -1); err != nil {
		return
	}
	delete(s.items, k)
	out := s.keys[:0]
	for _, x := range s.keys {
		if x != k {
			out = append(out, x)
		}
	}
	s.keys = out
}

func (s *memStorage) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.budget.reserve(-s.bytes, -len(s.items)); err != nil {
		return
	}
	s.items = make(map[string]string)
	s.keys = nil
	s.bytes = 0
}

func (s *memStorage) key(i int) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.keys) {
		return "", false
	}
	return s.keys[i], true
}

func (s *memStorage) length() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys)
}

const maxPendingTimers = 4096
const maxTimerCallbacksPerPump = 4096
const maxTimerFailuresPerPump = 16

// timerQueue is accessed only under the owning Runtime's ctxMu.
type timerQueue struct {
	nextID  int32
	pending map[int32]*timerEntry
	ready   timerHeap
	nesting int
}

type timerEntry struct {
	id        int32
	fn        *v8.Function
	due       time.Time
	delay     time.Duration
	repeating bool
	nesting   int
	index     int
	running   bool
}

func newTimerQueue() *timerQueue {
	return &timerQueue{pending: make(map[int32]*timerEntry)}
}

func (q *timerQueue) schedule(fn *v8.Function, delay time.Duration, repeating bool) int32 {
	if len(q.pending) >= maxPendingTimers || q.nextID == math.MaxInt32 {
		fn.Release()
		return 0
	}
	if q.nesting > 5 && delay < 4*time.Millisecond {
		delay = 4 * time.Millisecond
	}
	q.nextID++
	entry := &timerEntry{id: q.nextID, fn: fn, delay: delay, due: time.Now().Add(delay), repeating: repeating, nesting: q.nesting + 1}
	q.pending[entry.id] = entry
	heap.Push(&q.ready, entry)
	return entry.id
}

func (q *timerQueue) cancel(id int32) {
	entry := q.pending[id]
	if entry == nil {
		return
	}
	delete(q.pending, id)
	if entry.index >= 0 {
		heap.Remove(&q.ready, entry.index)
		entry.fn.Release()
	} else if !entry.running {
		entry.fn.Release()
	}
}

func (q *timerQueue) takeDue(now time.Time) *timerEntry {
	if len(q.ready) == 0 || q.ready[0].due.After(now) {
		return nil
	}
	return heap.Pop(&q.ready).(*timerEntry)
}

func (q *timerQueue) finish(entry *timerEntry) bool {
	if entry.repeating && q.pending[entry.id] == entry {
		if entry.nesting > 5 && entry.delay < 4*time.Millisecond {
			entry.delay = 4 * time.Millisecond
		}
		entry.nesting++
		entry.due = time.Now().Add(entry.delay)
		return true
	}
	delete(q.pending, entry.id)
	entry.fn.Release()
	return false
}

func (q *timerQueue) nextDelay() (time.Duration, bool) {
	if len(q.ready) == 0 {
		return 0, false
	}
	return max(0, time.Until(q.ready[0].due)), true
}

func (q *timerQueue) close() {
	for id, entry := range q.pending {
		entry.fn.Release()
		delete(q.pending, id)
	}
	q.ready = nil
}

// fireTimers runs due work only. Future timers do not block an Eval.
func (c *Context) fireTimers() error {
	var callbackErrors []error
	var omittedFailures int
	var firedIntervals map[*timerEntry]struct{}
	var pumpErr error
	processed := 0
	for processed < maxTimerCallbacksPerPump {
		if c.closed.Load() {
			pumpErr = context.Canceled
			break
		}
		if c.evalCtx != nil && c.evalCtx.Err() != nil {
			pumpErr = c.evalCtx.Err()
			break
		}
		now := time.Now()
		if len(c.timers.ready) == 0 || c.timers.ready[0].due.After(now) {
			break
		}
		if _, alreadyFired := firedIntervals[c.timers.ready[0]]; alreadyFired {
			break
		}
		entry := c.timers.takeDue(now)
		processed++
		previous := c.timers.nesting
		c.timers.nesting = entry.nesting
		receiver := c.v8ctx.Global()
		entry.running = true
		value, err := entry.fn.Call(receiver)
		entry.running = false
		receiver.Release()
		c.timers.nesting = previous
		if c.timers.finish(entry) {
			if firedIntervals == nil {
				firedIntervals = make(map[*timerEntry]struct{})
			}
			firedIntervals[entry] = struct{}{}
			heap.Push(&c.timers.ready, entry)
		}
		if value != nil {
			value.Release()
		}
		if err != nil {
			if len(callbackErrors) < maxTimerFailuresPerPump {
				callbackErrors = append(callbackErrors, fmt.Errorf("timer %d callback: %w", entry.id, formatJSError(err)))
			} else {
				omittedFailures++
			}
		}
	}
	if processed == maxTimerCallbacksPerPump {
		if delay, pending := c.timers.nextDelay(); pending && delay == 0 {
			if _, alreadyFired := firedIntervals[c.timers.ready[0]]; !alreadyFired {
				pumpErr = errors.Join(pumpErr, errors.New("timer callback budget exceeded"))
			}
		}
	}
	if omittedFailures > 0 {
		callbackErrors = append(callbackErrors, fmt.Errorf("%d additional timer callback failures omitted", omittedFailures))
	}
	return errors.Join(pumpErr, errors.Join(callbackErrors...))
}

func installWindow(iso *v8.Isolate, v8ctx *v8.Context, c *Context, pageURL string, nav NavigatorConfig) error {
	nav.applyDefaults()

	global := v8ctx.Global()

	// window === globalThis
	if err := global.Set("window", global); err != nil {
		return err
	}

	// location
	loc, err := buildLocation(c, pageURL)
	if err != nil {
		return err
	}
	if setErr := global.Set("location", loc); setErr != nil {
		return setErr
	}

	// navigator: when a startup snapshot is loaded, navigator already
	// exists with extras like userAgentData baked in. Set Go-bound
	// userAgent/language/platform/etc directly on the existing object
	// instead of replacing it. Otherwise build a fresh object.
	if existing, gerr := global.Get("navigator"); gerr == nil && existing.IsObject() {
		existingObj, _ := existing.AsObject()
		if existingObj != nil {
			if setErr := setNavigatorFields(iso, v8ctx, existingObj, nav); setErr != nil {
				return setErr
			}
		}
	} else {
		navObj, buildErr := buildNavigator(iso, v8ctx, nav)
		if buildErr != nil {
			return buildErr
		}
		if setErr := global.Set("navigator", navObj); setErr != nil {
			return setErr
		}
	}

	// localStorage + sessionStorage. Uses Runtime-cached templates so the
	// 6 callbacks per Storage are registered once per Isolate, not per
	// Context. Per-Context state lives on the receiver's internal field 0.
	lsObj, err := buildStorageCached(c, c.localStorage)
	if err != nil {
		return err
	}
	if setErr := global.Set("localStorage", lsObj); setErr != nil {
		return setErr
	}
	ssObj, err := buildStorageCached(c, c.sessionStorage)
	if err != nil {
		return err
	}
	if setErr := global.Set("sessionStorage", ssObj); setErr != nil {
		return setErr
	}

	// setTimeout / clearTimeout / setInterval / clearInterval
	return installTimers(iso, v8ctx, c)
}

func buildLocation(c *Context, raw string) (*v8.Value, error) {
	tmpl := cachedLocationTemplate(c)
	obj, err := tmpl.NewInstance(c.v8ctx)
	if err != nil {
		return nil, err
	}
	u, perr := url.Parse(raw)
	if perr != nil || u == nil {
		u = &url.URL{}
	}
	host := u.Host
	port := u.Port()
	hostname := u.Hostname()
	origin := ""
	if u.Scheme != "" && host != "" {
		origin = u.Scheme + "://" + host
	}
	pathname := u.Path
	if pathname == "" {
		pathname = "/"
	}
	search := ""
	if u.RawQuery != "" {
		search = "?" + u.RawQuery
	}
	hash := ""
	if u.Fragment != "" {
		hash = "#" + u.Fragment
	}
	// SetMany batches all 9 property writes into a single cgo crossing
	// (vs 9 individual ObjectSet calls). Keys are pre-CString'd at init
	// so we skip per-call CString allocations.
	_ = obj.SetManyPrepared(locationKeys, []interface{}{
		raw, withColon(u.Scheme), host, hostname, port, pathname, search, hash, origin,
	})
	return obj.Value, nil
}

var locationKeys = v8.PrepareKeys([]string{
	"href", "protocol", "host", "hostname", "port", "pathname", "search", "hash", "origin",
})

// cachedLocationTemplate returns the Runtime-shared ObjectTemplate used
// to create location objects. Caching saves one cgo NewObjectTemplate
// call per NewContext.
func cachedLocationTemplate(c *Context) *v8.ObjectTemplate {
	r := c.rt
	r.mu.Lock()
	if r.locationTemplate == nil {
		r.locationTemplate = v8.NewObjectTemplate(r.iso)
	}
	t := r.locationTemplate
	r.mu.Unlock()
	return t
}

func withColon(scheme string) string {
	if scheme == "" {
		return ""
	}
	return scheme + ":"
}

func buildNavigator(iso *v8.Isolate, v8ctx *v8.Context, nav NavigatorConfig) (*v8.Value, error) {
	tmpl := v8.NewObjectTemplate(iso)
	obj, err := tmpl.NewInstance(v8ctx)
	if err != nil {
		return nil, err
	}
	if err := setNavigatorFields(iso, v8ctx, obj, nav); err != nil {
		obj.Release()
		return nil, err
	}
	return obj.Value, nil
}

// setNavigatorFields writes the Go-bound navigator properties onto obj.
// Used by both fresh-build (buildNavigator) and merge-into-existing
// (snapshot-loaded) paths. Batches all 7 simple property writes into a
// single SetMany cgo crossing; the languages array still goes through
// individual SetIdx because v8go has no array batch helper.
func setNavigatorFields(iso *v8.Isolate, v8ctx *v8.Context, obj *v8.Object, nav NavigatorConfig) error {
	langs, err := v8ctx.NewNullPrototypeObject()
	if err != nil {
		return err
	}
	defer langs.Release()
	for i, l := range nav.Languages {
		if err := langs.SetIdx(uint32(i), l); err != nil {
			return err
		}
	}
	length, ok := checkedIntToInt32(len(nav.Languages))
	if !ok {
		return errors.New("navigator languages exceed JavaScript integer range")
	}
	if err := langs.Set("length", length); err != nil {
		return err
	}
	return obj.SetManyPrepared(navigatorKeys, []interface{}{
		nav.UserAgent, nav.Language, nav.Platform, langs, true, true, v8.Null(iso),
		nav.Vendor, int32(nav.HardwareConcurrency), nav.DeviceMemory, int32(nav.MaxTouchPoints),
	})
}

var navigatorKeys = v8.PrepareKeys([]string{
	"userAgent", "language", "platform", "languages", "onLine", "cookieEnabled", "doNotTrack",
	"vendor", "hardwareConcurrency", "deviceMemory", "maxTouchPoints",
})

// timerTemplates holds the cached setTimeout/clearTimeout function
// templates. setInterval reuses setTimeout's scheduling template with its
// recurring flag; clearInterval reuses clearTimeout's. Per-Context
// dispatch happens via Runtime.contextFor(info.Context()).
type timerTemplates struct {
	set   *v8.FunctionTemplate
	clear *v8.FunctionTemplate
}

func (r *Runtime) ensureTimerTemplates() *timerTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timerTemplates != nil {
		return r.timerTemplates
	}
	iso := r.iso
	r.timerTemplates = &timerTemplates{
		set:   v8.NewFunctionTemplate(iso, r.scheduleTimer),
		clear: v8.NewFunctionTemplate(iso, r.clearTimer),
	}
	return r.timerTemplates
}

func (r *Runtime) scheduleTimer(info *v8.FunctionCallbackInfo) *v8.Value {
	c := r.contextFor(info.Context())
	args := info.Args()
	defer info.This().Release()
	defer func() {
		for index := 1; index < len(args); index++ {
			args[index].Release()
		}
	}()
	if c == nil || len(args) != 3 {
		if len(args) > 0 {
			args[0].Release()
		}
		return r.callbackPrimitive(info, int32(0))
	}
	fn, err := args[0].AsFunction()
	if err != nil {
		args[0].Release()
		return r.callbackPrimitive(info, int32(0))
	}
	// The queue owns the function argument; the other callback handles retire here.
	delay := time.Duration(max(0, args[1].Int32())) * time.Millisecond
	return r.callbackPrimitive(info, c.timers.schedule(fn, delay, args[2].Boolean()))
}

func (r *Runtime) clearTimer(info *v8.FunctionCallbackInfo) *v8.Value {
	defer info.Release()
	c := r.contextFor(info.Context())
	args := info.Args()
	if c == nil || len(args) < 1 {
		return v8.Null(r.iso)
	}
	timerID, ok := checkedInt64ToInt32(args[0].Integer())
	if !ok {
		return v8.Null(r.iso)
	}
	c.timers.cancel(timerID)
	return v8.Null(r.iso)
}

func installTimers(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	tt := c.rt.ensureTimerTemplates()
	g := v8ctx.Global()
	defer g.Release()
	setFn := tt.set.GetFunction(v8ctx)
	defer setFn.Release()
	clearFn := tt.clear.GetFunction(v8ctx)
	defer clearFn.Release()
	if err := g.Set("__artemisSetTimer", setFn); err != nil {
		return err
	}
	if err := g.Set("__artemisClearTimer", clearFn); err != nil {
		return err
	}
	c.registerBootstrap("artemis-timers", timerBootstrap)
	return nil
}

const timerBootstrap = `
(() => {
  const owner = globalThis;
  const schedule = owner.__artemisSetTimer;
  const clear = owner.__artemisClearTimer;
  const apply = Reflect.apply, stringify = String, number = Number, evaluate = eval;
  const TypeFailure = TypeError, RangeFailure = RangeError;
  delete owner.__artemisSetTimer;
  delete owner.__artemisClearTimer;
  const makeTimer = repeat => function(handler, delay = 0, ...args) {
    if (arguments.length === 0) throw new TypeFailure('timer handler is required');
    let callback;
    if (typeof handler === 'function') {
      callback = () => apply(handler, owner, args);
    } else {
      if (typeof handler === 'symbol') throw new TypeFailure('timer handler requires a function or string');
      const code = stringify(handler);
      callback = () => (0, evaluate)(code);
    }
    const timeout = number(delay) >> 0;
    const id = schedule(callback, timeout > 0 ? timeout : 0, repeat);
    if (id === 0) throw new RangeFailure('timer admission limit exceeded');
    return id;
  };
  owner.setTimeout = makeTimer(false);
  owner.setInterval = makeTimer(true);
  owner.clearTimeout = owner.clearInterval = id => clear(number(id) >> 0);
})();
`
