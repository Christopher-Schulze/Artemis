package fixture

import (
	"crypto/subtle"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

const replayContentSecurityPolicy = "sandbox; default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

const AccessTokenHeader = "X-Artemis-Fixture-Token"

// Server wraps an httptest.Server with a deterministic fixture corpus.
// Routes are registered before or after the server starts; the Server is
// safe for use by multiple concurrent tests when each test creates its own
// instance.
type Server struct {
	srv             *httptest.Server
	mux             *http.ServeMux
	mu              sync.RWMutex
	scenarios       map[string]Scenario
	registeredBytes int64
	requests        chan struct{}
	closed          bool
	accessToken     string
	corpusData      []byte
	corpusScenarios []Scenario
}

// NewServer creates an empty fixture server.
func NewServer() *Server {
	mux := http.NewServeMux()
	s := &Server{
		mux:       mux,
		scenarios: make(map[string]Scenario),
		requests:  make(chan struct{}, maxFixtureConcurrentRequests),
	}
	mux.Handle("/", s)
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.handleRequest))
	s.srv.Config.ReadHeaderTimeout = 5 * time.Second
	s.srv.Config.ReadTimeout = 10 * time.Second
	s.srv.Config.MaxHeaderBytes = 32 << 10
	s.srv.Start()
	return s
}

// RequireAccessToken protects every route, including the index. It must be
// configured once before any scenario is registered; callers own token entropy.
func (s *Server) RequireAccessToken(token string) error {
	if s == nil || len(token) < 26 || len(token) > 128 || strings.Trim(token, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
		return fmt.Errorf("fixture: bounded base32 access token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.scenarios) != 0 || s.accessToken != "" {
		return fmt.Errorf("fixture: access token must precede registration and cannot change")
	}
	s.accessToken = token
	return nil
}

// NewServerWithDefaults creates a server and registers DefaultScenarios.
func NewServerWithDefaults() (*Server, error) {
	data, err := DefaultCorpusBytes()
	if err != nil {
		return nil, err
	}
	return NewServerFromCorpus(data)
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.srv.Close()
}

// BaseURL returns the server base URL (scheme://host:port).
func (s *Server) BaseURL() string { return s.srv.URL }

// URL returns an absolute URL for the given path.
func (s *Server) URL(path string) string {
	if path == "" {
		return s.srv.URL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.srv.URL + path
}

// HTTPServer returns the underlying httptest server.
func (s *Server) HTTPServer() *httptest.Server { return s.srv }

// PolicyConfig returns a network policy that allows the fixture server's
// private loopback address and port.
func (s *Server) PolicyConfig() network.PolicyConfig {
	ports := []int{80, 443}
	if u, err := url.Parse(s.srv.URL); err == nil && u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			ports = append(ports, p)
		}
	}
	sort.Ints(ports)
	return network.PolicyConfig{
		AllowPrivateNetworks: true,
		AllowedPorts:         ports,
	}
}

// Register adds a fully configured scenario to the server.
func (s *Server) Register(sc Scenario) error {
	return s.RegisterAll([]Scenario{sc})
}

// RegisterAll registers every scenario.
func (s *Server) RegisterAll(scenarios []Scenario) error {
	if s == nil || len(scenarios) == 0 || len(scenarios) > maxRegisteredScenarios {
		return fmt.Errorf("fixture: nonempty bounded registration batch required")
	}
	prepared := make([]Scenario, len(scenarios))
	var addedBytes int64
	for i, sc := range scenarios {
		value, size, err := prepareScenario(sc)
		if err != nil {
			return err
		}
		prepared[i] = value
		addedBytes += size
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(prepared) > maxRegisteredScenarios-len(s.scenarios) || addedBytes > maxRegisteredBytes-s.registeredBytes {
		return fmt.Errorf("fixture: closed owner or registration budget exceeded")
	}
	paths, ids := make(map[string]bool), make(map[string]bool)
	for _, sc := range s.scenarios {
		paths[sc.Path], ids[sc.ID] = true, true
	}
	for _, sc := range prepared {
		if paths[sc.Path] || ids[sc.ID] {
			return fmt.Errorf("fixture: duplicate path or ID for %q", sc.ID)
		}
		paths[sc.Path], ids[sc.ID] = true, true
	}
	for _, sc := range prepared {
		handler := sc.Handler
		if handler == nil {
			handler = s.contentHandler(sc)
		}
		s.mux.Handle(sc.Path, handler)
		s.scenarios[sc.Path] = sc
	}
	s.registeredBytes += addedBytes
	return nil
}

// RegisterHTML serves a static HTML body at the given path.
func (s *Server) RegisterHTML(path, html string) error {
	return s.Register(Scenario{
		ID:          strings.Trim(path, "/"),
		Path:        path,
		Kind:        KindHTML,
		ContentType: "text/html; charset=utf-8",
		Status:      http.StatusOK,
		HTML:        html,
	})
}

// RegisterRaw serves a static response with the given content type and status.
func (s *Server) RegisterRaw(path, contentType string, status int, body []byte) error {
	return s.Register(Scenario{
		ID:          strings.Trim(path, "/"),
		Path:        path,
		Kind:        KindUnknown,
		ContentType: contentType,
		Status:      status,
		HTML:        string(body),
	})
}

// RegisterHandler serves a custom handler at the given path.
func (s *Server) RegisterHandler(path string, h http.Handler) error {
	return s.Register(Scenario{
		ID:      strings.Trim(path, "/"),
		Path:    path,
		Kind:    KindUnknown,
		Handler: h,
	})
}

// Scenarios returns a sorted copy of the registered scenario list.
func (s *Server) Scenarios() []Scenario {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Scenario, 0, len(s.scenarios))
	for _, sc := range s.scenarios {
		out = append(out, cloneScenario(sc))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ScenarioByPath returns the scenario registered at path, or nil.
func (s *Server) ScenarioByPath(path string) *Scenario {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc, ok := s.scenarios[path]
	if !ok {
		return nil
	}
	copy := cloneScenario(sc)
	return &copy
}

// ScenarioByID returns the first scenario with the given ID, or nil.
func (s *Server) ScenarioByID(id string) *Scenario {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.scenarios {
		if sc.ID == id {
			copy := cloneScenario(sc)
			return &copy
		}
	}
	return nil
}

func (s *Server) contentHandler(sc Scenario) http.Handler {
	ct := sc.ContentType
	if ct == "" {
		ct = "text/html; charset=utf-8"
	}
	status := sc.Status
	if status == 0 {
		status = http.StatusOK
	}
	body := []byte(sc.HTML)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		if sc.inertReplay {
			w.Header().Set("Content-Security-Policy", replayContentSecurityPolicy)
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
		}
		w.WriteHeader(status)
		if _, err := w.Write(body); err != nil {
			return
		}
	})
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	closed := s.closed
	accessToken := s.accessToken
	handler, _ := s.mux.Handler(r)
	s.mu.RUnlock()
	if closed {
		http.Error(w, "fixture owner closed", http.StatusServiceUnavailable)
		return
	}
	if accessToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(AccessTokenHeader)), []byte(accessToken)) != 1 {
		http.NotFound(w, r)
		return
	}
	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	default:
		http.Error(w, "fixture request capacity exceeded", http.StatusServiceUnavailable)
		return
	}
	if r.ContentLength > fixtureUploadMaxBytes {
		http.Error(w, "fixture request body limit exceeded", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, fixtureUploadMaxBytes)
	handler.ServeHTTP(w, r)
}

// ServeHTTP implements the root index handler and 404 fallback.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	closed, accessToken := s.closed, s.accessToken
	s.mu.RUnlock()
	if closed || accessToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(AccessTokenHeader)), []byte(accessToken)) != 1 {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/" {
		s.indexHandler(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Artemis Fixture Server</title></head><body>"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "<h1>Artemis Fixture Server v%s</h1>", Version); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "<ul>"); err != nil {
		return
	}
	for _, sc := range s.Scenarios() {
		label := sc.ID
		if label == "" {
			label = sc.Path
		}
		if sc.Description != "" {
			label += " - " + sc.Description
		}
		if _, err := fmt.Fprintf(w, "<li><a href=\"%s\">%s</a> [%s]</li>\n",
			html.EscapeString(sc.Path), html.EscapeString(label), sc.Kind); err != nil {
			return
		}
	}
	if _, err := fmt.Fprint(w, "</ul></body></html>"); err != nil {
		return
	}
}
