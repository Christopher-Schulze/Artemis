package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cookie is one Chromium cookie entry (spec L4577).
type Cookie struct {
	Domain    string    `json:"domain"`
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	Path      string    `json:"path"`
	ExpiresAt time.Time `json:"expires_at"`
	Secure    bool      `json:"secure"`
	HTTPOnly  bool      `json:"http_only"`
	SameSite  string    `json:"same_site"`
}

// CookieStore manages cookie export/import/cleanup (spec L4577).
// In production cookies live in Chromium's Default/Cookies SQLite; this
// store provides the JSON export/import and cleanup operations that
// RightsProcessor.FindAllData and Art.17 erasure call.
type CookieStore struct {
	mu      sync.Mutex
	cookies map[string]*Cookie // keyed by domain|name|path
}

// NewCookieStore creates an empty cookie store.
func NewCookieStore() *CookieStore {
	return &CookieStore{cookies: make(map[string]*Cookie)}
}

// Add inserts or replaces a cookie.
func (s *CookieStore) Add(c *Cookie) {
	if s == nil || c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookies[cookieKey(c)] = c
}

// ListCookies returns all cookies, optionally filtered by domain.
func (s *CookieStore) ListCookies(domain string) []Cookie {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Cookie, 0, len(s.cookies))
	for _, c := range s.cookies {
		if domain != "" && c.Domain != domain {
			continue
		}
		out = append(out, *c)
	}
	return out
}

// ExportCookies serializes all cookies (or domain-filtered) to JSON
// (spec L4577).
func (s *CookieStore) ExportCookies(domain string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("cookie store: nil")
	}
	cookies := s.ListCookies(domain)
	return json.MarshalIndent(cookies, "", "  ")
}

// ImportCookies parses JSON and replaces cookies for the matching domains
// (spec L4577).
func (s *CookieStore) ImportCookies(data []byte) error {
	if s == nil {
		return errors.New("cookie store: nil")
	}
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return fmt.Errorf("cookie store: import parse: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Remove existing cookies for imported domains, then add.
	domains := make(map[string]struct{})
	for _, c := range cookies {
		domains[c.Domain] = struct{}{}
	}
	for k, c := range s.cookies {
		if _, ok := domains[c.Domain]; ok {
			delete(s.cookies, k)
		}
	}
	for i := range cookies {
		c := cookies[i]
		s.cookies[cookieKey(&c)] = &c
	}
	return nil
}

// ClearExpired removes cookies whose ExpiresAt is in the past (spec L4577).
func (s *CookieStore) ClearExpired() int {
	if s == nil {
		return 0
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for k, c := range s.cookies {
		if !c.ExpiresAt.IsZero() && c.ExpiresAt.Before(now) {
			delete(s.cookies, k)
			removed++
		}
	}
	return removed
}

// ClearDomain removes all cookies for a domain (spec L4577: Art.17 calls
// ClearDomain when no retention block exists).
func (s *CookieStore) ClearDomain(domain string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for k, c := range s.cookies {
		if c.Domain == domain {
			delete(s.cookies, k)
			removed++
		}
	}
	return removed
}

// ClearAll removes all cookies (spec L4577).
func (s *CookieStore) ClearAll() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.cookies)
	s.cookies = make(map[string]*Cookie)
	return n
}

// ExportToFile writes exported cookies to a JSON file.
func (s *CookieStore) ExportToFile(path string, domain string) error {
	data, err := s.ExportCookies(domain)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("cookie store: mkdir: %w", err)
		}
	}
	return os.WriteFile(path, data, 0o600)
}

// ImportFromFile reads cookies from a JSON file.
func (s *CookieStore) ImportFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cookie store: read: %w", err)
	}
	return s.ImportCookies(data)
}

func cookieKey(c *Cookie) string {
	return strings.ToLower(c.Domain) + "|" + c.Name + "|" + c.Path
}
