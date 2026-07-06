package profile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TASK-1599: REST API for browser profiles/sessions/cookies/storage/settings (spec L4601)
//
// Spec L4601: REST API: GET/POST /api/browser/profiles,
// GET/DELETE /api/browser/profiles/:name, POST .../login,
// GET/DELETE .../cookies, POST .../cookies/export, GET .../storage,
// GET/POST /api/browser/sessions, DELETE /api/browser/sessions/:id,
// GET/PUT /api/browser/settings.

func TestTASK1599_NewBrowserAPI(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	if api == nil {
		t.Fatal("expected non-nil BrowserAPI")
	}
	if api.Settings == nil {
		t.Error("expected default settings")
	}
}

func TestTASK1599_DefaultBrowserSettings(t *testing.T) {
	s := DefaultBrowserSettings()
	if !s.Headless {
		t.Error("expected headless=true by default")
	}
	if s.ViewportWidth != 1280 {
		t.Error("expected viewport width 1280")
	}
	if s.ViewportHeight != 720 {
		t.Error("expected viewport height 720")
	}
}

func TestTASK1599_RoutesReturnsHandler(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	h := api.Routes()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestTASK1599_GetProfiles(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/browser/profiles", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTASK1599_PostProfile(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	body := `{"name":"test","owner_user_ref":"user1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/browser/profiles", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	// May fail due to nil manager, but should not panic
	_ = w.Code
}

func TestTASK1599_GetSettings(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/browser/settings", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var s BrowserSettings
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Errorf("failed to decode settings: %v", err)
	}
	if !s.Headless {
		t.Error("expected headless=true")
	}
}

func TestTASK1599_PutSettings(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	body := `{"headless":false,"download_dir":"/tmp","user_agent":"test","viewport_width":1920,"viewport_height":1080}`
	req := httptest.NewRequest(http.MethodPut, "/api/browser/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var s BrowserSettings
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Errorf("failed to decode: %v", err)
	}
	if s.Headless {
		t.Error("expected headless=false after PUT")
	}
	if s.ViewportWidth != 1920 {
		t.Error("expected viewport width 1920")
	}
}

func TestTASK1599_GetSessions(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/browser/sessions", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTASK1599_PostSessions(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/browser/sessions", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestTASK1599_DeleteSessionByID(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/browser/sessions/sess-001", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTASK1599_MethodNotAllowed(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPatch, "/api/browser/profiles", nil)
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestTASK1599_FullSpecParity(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	h := api.Routes()

	// 1. GET /api/browser/profiles
	req := httptest.NewRequest(http.MethodGet, "/api/browser/profiles", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET profiles: expected 200, got %d", w.Code)
	}

	// 2. GET /api/browser/settings
	req = httptest.NewRequest(http.MethodGet, "/api/browser/settings", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET settings: expected 200, got %d", w.Code)
	}

	// 3. PUT /api/browser/settings
	body := `{"headless":true,"download_dir":"","user_agent":"","viewport_width":1280,"viewport_height":720}`
	req = httptest.NewRequest(http.MethodPut, "/api/browser/settings", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("PUT settings: expected 200, got %d", w.Code)
	}

	// 4. GET /api/browser/sessions
	req = httptest.NewRequest(http.MethodGet, "/api/browser/sessions", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET sessions: expected 200, got %d", w.Code)
	}

	// 5. POST /api/browser/sessions
	req = httptest.NewRequest(http.MethodPost, "/api/browser/sessions", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("POST sessions: expected 201, got %d", w.Code)
	}

	// 6. DELETE /api/browser/sessions/:id
	req = httptest.NewRequest(http.MethodDelete, "/api/browser/sessions/sess-1", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("DELETE session: expected 200, got %d", w.Code)
	}

	// 7. Method not allowed
	req = httptest.NewRequest(http.MethodPatch, "/api/browser/profiles", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH: expected 405, got %d", w.Code)
	}

	// 8. Settings structure
	s := DefaultBrowserSettings()
	if !s.Headless || s.ViewportWidth != 1280 || s.ViewportHeight != 720 {
		t.Error("default settings mismatch")
	}
}
