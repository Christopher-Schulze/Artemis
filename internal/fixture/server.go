package fixture

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Artemis/network"
)

// Server wraps an httptest.Server with a deterministic fixture corpus.
// Routes are registered before or after the server starts; the Server is
// safe for use by multiple concurrent tests when each test creates its own
// instance.
type Server struct {
	srv       *httptest.Server
	mux       *http.ServeMux
	mu        sync.RWMutex
	scenarios map[string]Scenario
}

// NewServer creates an empty fixture server.
func NewServer() *Server {
	mux := http.NewServeMux()
	s := &Server{
		mux:       mux,
		scenarios: make(map[string]Scenario),
	}
	s.srv = httptest.NewServer(mux)
	mux.Handle("/", s)
	return s
}

// NewServerWithDefaults creates a server and registers DefaultScenarios.
func NewServerWithDefaults() *Server {
	s := NewServer()
	s.RegisterAll(DefaultScenarios())
	return s
}

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

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
func (s *Server) Register(sc Scenario) {
	if sc.Path == "" {
		sc.Path = "/" + sc.ID
	}
	if !strings.HasPrefix(sc.Path, "/") {
		sc.Path = "/" + sc.Path
	}

	s.mu.Lock()
	s.scenarios[sc.Path] = sc
	s.mu.Unlock()

	if sc.Handler != nil {
		s.mux.Handle(sc.Path, sc.Handler)
		return
	}

	s.mux.Handle(sc.Path, s.contentHandler(sc))
}

// RegisterAll registers every scenario.
func (s *Server) RegisterAll(scenarios []Scenario) {
	for _, sc := range scenarios {
		s.Register(sc)
	}
}

// RegisterHTML serves a static HTML body at the given path.
func (s *Server) RegisterHTML(path, html string) {
	s.Register(Scenario{
		ID:          strings.Trim(path, "/"),
		Path:        path,
		Kind:        KindHTML,
		ContentType: "text/html; charset=utf-8",
		Status:      http.StatusOK,
		HTML:        html,
	})
}

// RegisterRaw serves a static response with the given content type and status.
func (s *Server) RegisterRaw(path, contentType string, status int, body []byte) {
	s.Register(Scenario{
		ID:          strings.Trim(path, "/"),
		Path:        path,
		Kind:        KindUnknown,
		ContentType: contentType,
		Status:      status,
		HTML:        string(body),
	})
}

// RegisterHandler serves a custom handler at the given path.
func (s *Server) RegisterHandler(path string, h http.Handler) {
	s.Register(Scenario{
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
		out = append(out, sc)
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
	return &sc
}

// ScenarioByID returns the first scenario with the given ID, or nil.
func (s *Server) ScenarioByID(id string) *Scenario {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.scenarios {
		if sc.ID == id {
			return &sc
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
		w.WriteHeader(status)
		w.Write(body)
	})
}

// ServeHTTP implements the root index handler and 404 fallback.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		s.indexHandler(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "<!doctype html><html><head><title>Artemis Fixture Server</title></head><body>")
	fmt.Fprintf(w, "<h1>Artemis Fixture Server v%s</h1>", Version)
	fmt.Fprint(w, "<ul>")
	for _, sc := range s.Scenarios() {
		label := sc.ID
		if label == "" {
			label = sc.Path
		}
		if sc.Description != "" {
			label += " - " + sc.Description
		}
		fmt.Fprintf(w, "<li><a href=\"%s\">%s</a> [%s]</li>\n",
			html.EscapeString(sc.Path), html.EscapeString(label), sc.Kind)
	}
	fmt.Fprint(w, "</ul></body></html>")
}
