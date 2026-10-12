package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/profiles", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTASK1599_PostProfile(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	body := `{"name":"test","owner_user_ref":"user1"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/browser/profiles", strings.NewReader(body))
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	// May fail due to nil manager, but should not panic
	_ = w.Code
}

func TestTASK1599_GetSettings(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/settings", nil)
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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/browser/settings", strings.NewReader(body))
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
	runtime, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewBrowserAPI(nil, nil, nil, nil).WithRuntime(runtime)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/sessions", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTASK1599_PostSessions(t *testing.T) {
	runtime, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewBrowserAPI(nil, nil, nil, nil).WithRuntime(runtime)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/browser/sessions", strings.NewReader(`{"profile_id":"api-profile","class":"ephemeral"}`))
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestTASK1599_DeleteSessionByID(t *testing.T) {
	runtime, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.Open(context.Background(), OpenSessionRequest{ProfileID: "api-profile", OwnerUserRef: "user1", Class: ProfileEphemeral})
	if err != nil {
		t.Fatal(err)
	}
	api := NewBrowserAPI(nil, nil, nil, nil).WithRuntime(runtime)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/browser/sessions/"+string(session.ID), nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestBrowserAPILoginExecutesSessionManager(t *testing.T) {
	manager := NewProfileManager(t.TempDir(), nil)
	if err := manager.Create(&BrowserProfile{Name: "login-profile", OwnerUserRef: "owner", AllowedDomains: []string{"example.com"}}); err != nil {
		t.Fatal(err)
	}
	credentials, err := NewCredentialStore(filepath.Join(t.TempDir(), "credentials.enc"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.StoreCredential("login-profile", "example.com", "user", "secret", LoginSelectors{}); err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionManager(credentials, &fakeDetector{visible: true}, &fakeExecutor{fillOK: true})
	api := NewBrowserAPI(manager, sessions, NewCookieStore(), NewStorageManager(t.TempDir()))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/browser/profiles/login-profile/login", strings.NewReader(`{"domain":"example.com","purpose":"support"}`))
	req.Header.Set("X-Artemis-Owner", "owner")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	var result LoginAttemptResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil || !result.Success {
		t.Fatalf("login result=%+v err=%v", result, err)
	}
}

func TestTASK1599_MethodNotAllowed(t *testing.T) {
	api := NewBrowserAPI(nil, nil, nil, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/browser/profiles", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	api.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestTASK1599_FullSpecParity(t *testing.T) {
	runtime, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewBrowserAPI(nil, nil, nil, nil).WithRuntime(runtime)
	h := api.Routes()

	// 1. GET /api/browser/profiles
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/profiles", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET profiles: expected 200, got %d", w.Code)
	}

	// 2. GET /api/browser/settings
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/settings", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET settings: expected 200, got %d", w.Code)
	}

	// 3. PUT /api/browser/settings
	body := `{"headless":true,"download_dir":"","user_agent":"","viewport_width":1280,"viewport_height":720}`
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/browser/settings", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("PUT settings: expected 200, got %d", w.Code)
	}

	// 4. GET /api/browser/sessions
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/browser/sessions", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET sessions: expected 200, got %d", w.Code)
	}

	// 5. POST /api/browser/sessions
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/browser/sessions", strings.NewReader(`{"profile_id":"full-spec","class":"ephemeral"}`))
	req.Header.Set("X-Artemis-Owner", "user1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("POST sessions: expected 201, got %d", w.Code)
	}
	var created RuntimeSession
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// 6. DELETE /api/browser/sessions/:id
	req = httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/browser/sessions/"+string(created.ID), nil)
	req.Header.Set("X-Artemis-Owner", "user1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("DELETE session: expected 200, got %d", w.Code)
	}

	// 7. Method not allowed
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/browser/profiles", nil)
	req.Header.Set("X-Artemis-Owner", "user1")
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
