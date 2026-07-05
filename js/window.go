package js

import (
	"net/url"
	"sync"

	v8 "rogchap.com/v8go"
)

// NavigatorConfig configures the values returned by window.navigator.
// Zero values are replaced by Artemis defaults.
type NavigatorConfig struct {
	UserAgent string
	Language  string
	Languages []string
	Platform  string
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
}

// memStorage is an in-memory implementation of the Storage WebAPI.
type memStorage struct {
	mu    sync.Mutex
	keys  []string
	items map[string]string
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

func (s *memStorage) setItem(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[k]; !ok {
		s.keys = append(s.keys, k)
	}
	s.items[k] = v
}

func (s *memStorage) removeItem(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[k]; !ok {
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
	s.items = make(map[string]string)
	s.keys = nil
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

// timerQueue holds pending setTimeout callbacks. Each Context owns one.
type timerQueue struct {
	mu      sync.Mutex
	nextID  int32
	pending []timerEntry
}

type timerEntry struct {
	id      int32
	fn      *v8.Function
	cleared bool
}

func newTimerQueue() *timerQueue {
	return &timerQueue{}
}

func (q *timerQueue) schedule(fn *v8.Function) int32 {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	id := q.nextID
	q.pending = append(q.pending, timerEntry{id: id, fn: fn})
	return id
}

func (q *timerQueue) cancel(id int32) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.pending {
		if q.pending[i].id == id {
			q.pending[i].cleared = true
			return
		}
	}
}

// drain returns all pending non-cancelled timers and resets the queue.
func (q *timerQueue) drain() []timerEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	out := make([]timerEntry, 0, len(q.pending))
	for _, t := range q.pending {
		if !t.cleared {
			out = append(out, t)
		}
	}
	q.pending = nil
	return out
}

// fireTimers drains the queue and calls each callback. Repeated until
// empty so timers that schedule timers run too. Bounded loop guards
// against runaway loops.
func (c *Context) fireTimers() {
	const maxRounds = 64
	for i := 0; i < maxRounds; i++ {
		batch := c.timers.drain()
		if len(batch) == 0 {
			return
		}
		for _, t := range batch {
			_, _ = t.fn.Call(c.v8ctx.Global())
		}
	}
}

func installWindow(iso *v8.Isolate, v8ctx *v8.Context, c *Context, pageURL string, nav NavigatorConfig) error {
	nav.applyDefaults()

	global := v8ctx.Global()

	// window === globalThis
	if err := global.Set("window", global); err != nil {
		return err
	}

	// window EventTarget plumbing: addEventListener/removeEventListener/
	// dispatchEvent backed by a JS-side listener Map per Context. This is
	// what cross-Context postMessage delivery hooks into to fire 'message'
	// events on the iframe's window.
	c.registerBootstrap("artemis-window-events", inlineWindowEventsBootstrap)

	// location
	loc, err := buildLocation(c, pageURL)
	if err != nil {
		return err
	}
	if err := global.Set("location", loc); err != nil {
		return err
	}

	// navigator: when a startup snapshot is loaded, navigator already
	// exists with extras like userAgentData baked in. Set Go-bound
	// userAgent/language/platform/etc directly on the existing object
	// instead of replacing it. Otherwise build a fresh object.
	if existing, gerr := global.Get("navigator"); gerr == nil && existing.IsObject() {
		existingObj, _ := existing.AsObject()
		if existingObj != nil {
			if err := setNavigatorFields(iso, v8ctx, existingObj, nav); err != nil {
				return err
			}
		}
	} else {
		navObj, err := buildNavigator(iso, v8ctx, nav)
		if err != nil {
			return err
		}
		if err := global.Set("navigator", navObj); err != nil {
			return err
		}
	}

	// localStorage + sessionStorage. Uses Runtime-cached templates so the
	// 6 callbacks per Storage are registered once per Isolate, not per
	// Context. Per-Context state lives on the receiver's internal field 0.
	lsObj, err := buildStorageCached(c, c.localStorage)
	if err != nil {
		return err
	}
	if err := global.Set("localStorage", lsObj); err != nil {
		return err
	}
	ssObj, err := buildStorageCached(c, c.sessionStorage)
	if err != nil {
		return err
	}
	if err := global.Set("sessionStorage", ssObj); err != nil {
		return err
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
	langs, err := v8.NewObjectTemplate(iso).NewInstance(v8ctx)
	if err != nil {
		return err
	}
	for i, l := range nav.Languages {
		_ = langs.SetIdx(uint32(i), l)
	}
	_ = langs.Set("length", int32(len(nav.Languages)))
	return obj.SetManyPrepared(navigatorKeys, []interface{}{
		nav.UserAgent, nav.Language, nav.Platform, langs, true, true, "1",
	})
}

var navigatorKeys = v8.PrepareKeys([]string{
	"userAgent", "language", "platform", "languages", "onLine", "cookieEnabled", "doNotTrack",
})

func buildStorage(iso *v8.Isolate, v8ctx *v8.Context, s *memStorage) (*v8.Value, error) {
	tmpl := v8.NewObjectTemplate(iso)
	obj, err := tmpl.NewInstance(v8ctx)
	if err != nil {
		return nil, err
	}

	getItem := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 1 {
			return v8.Null(iso)
		}
		v, ok := s.getItem(args[0].String())
		if !ok {
			return v8.Null(iso)
		}
		return mustValue(iso, v)
	})
	_ = obj.Set("getItem", getItem.GetFunction(v8ctx))

	setItem := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 2 {
			return v8.Null(iso)
		}
		s.setItem(args[0].String(), args[1].String())
		// length getter is a snapshot; rewrite after each setItem so
		// callers reading .length see the new value.
		return v8.Null(iso)
	})
	_ = obj.Set("setItem", setItem.GetFunction(v8ctx))

	removeItem := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 1 {
			return v8.Null(iso)
		}
		s.removeItem(args[0].String())
		return v8.Null(iso)
	})
	_ = obj.Set("removeItem", removeItem.GetFunction(v8ctx))

	clear := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s.clear()
		return v8.Null(iso)
	})
	_ = obj.Set("clear", clear.GetFunction(v8ctx))

	key := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 1 {
			return v8.Null(iso)
		}
		idx := int(args[0].Integer())
		k, ok := s.key(idx)
		if !ok {
			return v8.Null(iso)
		}
		return mustValue(iso, k)
	})
	_ = obj.Set("key", key.GetFunction(v8ctx))

	// `length` is a value, snapshot at install. Most code accesses it
	// after a setItem; in 004d we accept that the value goes stale
	// inside a single eval boundary and is refreshed on the next eval
	// when the global is rebuilt - except we do not rebuild globals.
	// Workaround: install a function getter via a getter Object pattern
	// would require accessor properties (not exposed by v8go on Object).
	// Pragmatic compromise: add a `lengthOf()` method that always
	// returns live length. Document the trap for `length`.
	_ = obj.Set("length", int32(s.length()))

	lengthOf := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		return mustValue(iso, int32(s.length()))
	})
	_ = obj.Set("lengthOf", lengthOf.GetFunction(v8ctx))
	return obj.Value, nil
}

// timerTemplates holds the cached setTimeout/clearTimeout function
// templates. setInterval reuses setTimeout's template (we don't model
// recurring timers); clearInterval reuses clearTimeout's. Per-Context
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
		set: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return mustValue(iso, int32(0))
			}
			fn, err := args[0].AsFunction()
			if err != nil {
				return mustValue(iso, int32(0))
			}
			return mustValue(iso, c.timers.schedule(fn))
		}),
		clear: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			c.timers.cancel(int32(args[0].Integer()))
			return v8.Null(iso)
		}),
	}
	return r.timerTemplates
}

func installTimers(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	tt := c.rt.ensureTimerTemplates()
	g := v8ctx.Global()
	setFn := tt.set.GetFunction(v8ctx)
	clearFn := tt.clear.GetFunction(v8ctx)
	if err := g.Set("setTimeout", setFn); err != nil {
		return err
	}
	if err := g.Set("setInterval", setFn); err != nil {
		return err
	}
	if err := g.Set("clearTimeout", clearFn); err != nil {
		return err
	}
	return g.Set("clearInterval", clearFn)
}
