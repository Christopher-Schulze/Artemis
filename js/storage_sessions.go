package js

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"

	"golang.org/x/net/idna"
)

const (
	MaxStorageBytes        = 5 * 1024 * 1024
	MaxStorageKeys         = 16_384
	MaxStorageOrigins      = 64
	MaxStoragePartitions   = 1024
	MaxRuntimeStorageBytes = 64 * 1024 * 1024
	MaxRuntimeStorageKeys  = 65_536
)

type storageIdentity struct{ session, origin string }

// StorageSnapshot returns independent copies of this document's origin stores.
func (c *Context) StorageSnapshot() (map[string]string, map[string]string) {
	c.rt.ctxMu.Lock()
	defer c.rt.ctxMu.Unlock()
	return c.localStorage.snapshot(), c.sessionStorage.snapshot()
}

func (s *memStorage) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string]string, len(s.items))
	for key, value := range s.items {
		values[key] = value
	}
	return values
}

// SessionLocalStorageSnapshot reads retained owner state without creating a partition.
func (r *Runtime) SessionLocalStorageSnapshot(sessionID, rawURL string) map[string]string {
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	store := r.localStores[storageIdentity{sessionID, storageOrigin(rawURL)}]
	if store == nil {
		return nil
	}
	return store.snapshot()
}

type pageStorage struct{ local, session map[string]*memStorage }

type storageBudget struct {
	mu          sync.Mutex
	bytes, keys int
}

func (b *storageBudget) reserve(bytes, keys int) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bytes+bytes > MaxRuntimeStorageBytes || b.keys+keys > MaxRuntimeStorageKeys {
		return errors.New("web storage runtime quota exceeded")
	}
	b.bytes += bytes
	b.keys += keys
	return nil
}

func (r *Runtime) newStorage() *memStorage {
	store := newMemStorage()
	store.budget = r.storageBudget
	return store
}

// contextStorage is called while ctxMu serializes context construction.
func (r *Runtime) contextStorage(rawURL string, opts *ContextOpts) (*memStorage, *memStorage, error) {
	if opts.pageStorage == nil {
		opts.pageStorage = &pageStorage{local: make(map[string]*memStorage), session: make(map[string]*memStorage)}
	}
	origin := storageOrigin(rawURL)
	if origin == "" {
		return r.newStorage(), r.newStorage(), nil
	}
	if opts.pageStorage.session[origin] == nil {
		if len(opts.pageStorage.session) >= MaxStorageOrigins {
			return nil, nil, errors.New("web storage page origin limit exceeded")
		}
		opts.pageStorage.session[origin] = r.newStorage()
	}
	if opts.SessionID == "" {
		if opts.pageStorage.local[origin] == nil {
			opts.pageStorage.local[origin] = r.newStorage()
		}
		return opts.pageStorage.local[origin], opts.pageStorage.session[origin], nil
	}
	local, err := r.sessionLocalStorage(opts.SessionID, origin)
	return local, opts.pageStorage.session[origin], err
}

func (r *Runtime) sessionLocalStorage(sessionID, origin string) (*memStorage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	identity := storageIdentity{sessionID, origin}
	if store := r.localStores[identity]; store != nil {
		return store, nil
	}
	count := 0
	for key := range r.localStores {
		if key.session == sessionID {
			count++
		}
	}
	if count >= MaxStorageOrigins || len(r.localStores) >= MaxStoragePartitions {
		return nil, errors.New("web storage session origin limit exceeded")
	}
	if r.localStores == nil {
		r.localStores = make(map[storageIdentity]*memStorage)
	}
	store := r.newStorage()
	r.localStores[identity] = store
	return store, nil
}

// ReleaseSessionStorage clears retained origins after all owned pages stop.
func (r *Runtime) ReleaseSessionStorage(sessionID string) {
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, store := range r.localStores {
		if key.session == sessionID {
			store.clear()
			delete(r.localStores, key)
		}
	}
}

func storageOrigin(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) == nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			return ""
		}
		host = ascii
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func storageBytes(key, value string) int {
	bytes := 0
	for _, text := range []string{key, value} {
		for _, char := range text {
			if char > 0xffff {
				bytes += 4
			} else {
				bytes += 2
			}
		}
	}
	return bytes
}

var ErrDocumentCookieAuthority = errors.New("document cookies require non-HTTP cookie authority")

type documentCookieAccess interface {
	DocumentCookies(*url.URL) []*http.Cookie
	SetDocumentCookies(*url.URL, []*http.Cookie)
}

func documentCookies(jar http.CookieJar, rawURL string) (func() string, func(string), error) {
	owner, ok := jar.(documentCookieAccess)
	if !ok {
		return nil, nil, ErrDocumentCookieAuthority
	}
	value := reflect.ValueOf(owner)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		if value.IsNil() {
			return nil, nil, ErrDocumentCookieAuthority
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	get := func() string {
		cookies := owner.DocumentCookies(u)
		parts := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
		return strings.Join(parts, "; ")
	}
	set := func(line string) {
		response := http.Response{Header: http.Header{"Set-Cookie": []string{line}}}
		owner.SetDocumentCookies(u, response.Cookies())
	}
	return get, set, nil
}
