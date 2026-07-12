package profile

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// api.go (spec L4601: REST API for browser profiles, sessions, cookies, storage, settings).
//
// REST API endpoints:
//   GET/POST /api/browser/profiles
//   GET/DELETE /api/browser/profiles/:name
//   POST /api/browser/profiles/:name/login
//   GET/DELETE /api/browser/profiles/:name/cookies
//   POST /api/browser/profiles/:name/cookies/export
//   GET /api/browser/profiles/:name/storage
//   GET/POST /api/browser/sessions
//   DELETE /api/browser/sessions/:id
//   GET/PUT /api/browser/settings

// BrowserAPI is the REST API handler for browser profile/session/cookie/storage
// operations (spec L4601).
type BrowserAPI struct {
	mu       sync.RWMutex
	Manager  *ProfileManager
	Sessions *SessionManager
	Cookies  *CookieStore
	Storage  *StorageManager
	Settings *BrowserSettings
	Runtime  *RuntimeManager
}

// WithRuntime binds the authoritative session runtime to the REST surface.
func (a *BrowserAPI) WithRuntime(runtime *RuntimeManager) *BrowserAPI {
	a.Runtime = runtime
	return a
}

// BrowserSettings holds browser-level settings (spec L4601: GET/PUT /api/browser/settings).
type BrowserSettings struct {
	Headless       bool   `json:"headless"`
	DownloadDir    string `json:"download_dir"`
	UserAgent      string `json:"user_agent"`
	ViewportWidth  int    `json:"viewport_width"`
	ViewportHeight int    `json:"viewport_height"`
}

// DefaultBrowserSettings returns default browser settings.
func DefaultBrowserSettings() *BrowserSettings {
	return &BrowserSettings{
		Headless:       true,
		DownloadDir:    "",
		UserAgent:      "",
		ViewportWidth:  1280,
		ViewportHeight: 720,
	}
}

// NewBrowserAPI creates a new BrowserAPI handler.
func NewBrowserAPI(manager *ProfileManager, sessions *SessionManager, cookies *CookieStore, storage *StorageManager) *BrowserAPI {
	return &BrowserAPI{
		Manager:  manager,
		Sessions: sessions,
		Cookies:  cookies,
		Storage:  storage,
		Settings: DefaultBrowserSettings(),
	}
}

// Routes returns the HTTP handler with all REST API routes registered (spec L4601).
func (a *BrowserAPI) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/browser/profiles", a.handleProfiles)
	mux.HandleFunc("/api/browser/profiles/", a.handleProfileByName)
	mux.HandleFunc("/api/browser/sessions", a.handleSessions)
	mux.HandleFunc("/api/browser/sessions/", a.handleSessionByID)
	mux.HandleFunc("/api/browser/settings", a.handleSettings)
	return mux
}

func (a *BrowserAPI) handleProfiles(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.Header.Get("X-Artemis-Owner"))
	if owner == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "owner required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		profiles := a.Manager.List(owner)
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		var p BrowserProfile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		p.OwnerUserRef = owner
		if err := a.Manager.Create(&p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, p)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *BrowserAPI) handleProfileByName(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.Header.Get("X-Artemis-Owner"))
	if owner == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "owner required"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/browser/profiles/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "profile name required"})
		return
	}
	if a.Manager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "profile manager unavailable"})
		return
	}
	authorizedProfile, accessErr := a.Manager.Get(name, owner)
	if accessErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": accessErr.Error()})
		return
	}
	if len(parts) == 2 {
		sub := parts[1]
		switch {
		case sub == "login" && r.Method == http.MethodPost:
			if a.Sessions == nil || a.Manager == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "login runtime unavailable"})
				return
			}
			var request struct {
				Domain  string `json:"domain"`
				Purpose string `json:"purpose"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
				return
			}
			result, err := a.Sessions.AutoLogin(r.Context(), authorizedProfile.Name, request.Domain, request.Purpose, authorizedProfile.AllowedDomains)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			if !result.Success {
				writeJSON(w, http.StatusConflict, result)
				return
			}
			writeJSON(w, http.StatusOK, result)
		case sub == "cookies":
			a.handleCookies(w, r, name)
		case sub == "cookies/export" && r.Method == http.MethodPost:
			a.handleCookiesExport(w, r, name)
		case sub == "storage" && r.Method == http.MethodGet:
			a.handleStorage(w, r, name)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := a.Manager.Get(name, owner)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if err := a.Manager.Delete(name, owner); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *BrowserAPI) handleCookies(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		cookies := a.Cookies.ListCookies("")
		writeJSON(w, http.StatusOK, cookies)
	case http.MethodDelete:
		deleted := a.Cookies.ClearDomain("")
		writeJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *BrowserAPI) handleCookiesExport(w http.ResponseWriter, r *http.Request, name string) {
	data, err := a.Cookies.ExportCookies("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *BrowserAPI) handleStorage(w http.ResponseWriter, r *http.Request, name string) {
	entries := a.Storage.ListLocalStorage("")
	writeJSON(w, http.StatusOK, entries)
}

func (a *BrowserAPI) handleSessions(w http.ResponseWriter, r *http.Request) {
	if a.Runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session runtime unavailable"})
		return
	}
	owner := strings.TrimSpace(r.Header.Get("X-Artemis-Owner"))
	if owner == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "owner required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.Runtime.List(owner))
	case http.MethodPost:
		var request OpenSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		request.OwnerUserRef = owner
		if a.Manager != nil {
			profile, err := a.Manager.GetByID(request.ProfileID, owner)
			if err != nil {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			request.DataDir = profile.DataDir
		}
		session, err := a.Runtime.Open(r.Context(), request)
		if err != nil {
			writeRuntimeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, session)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *BrowserAPI) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/browser/sessions/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id required"})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if a.Runtime == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session runtime unavailable"})
			return
		}
		owner := strings.TrimSpace(r.Header.Get("X-Artemis-Owner"))
		if err := a.Runtime.Close(r.Context(), SessionID(id), owner); err != nil {
			writeRuntimeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "closed", "id": id})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		switch runtimeErr.Class {
		case FailureInvalid:
			status = http.StatusBadRequest
		case FailureDenied:
			status = http.StatusForbidden
		case FailureNotFound:
			status = http.StatusNotFound
		case FailureConflict, FailureLimit:
			status = http.StatusConflict
		case FailureCorrupt:
			status = http.StatusUnprocessableEntity
		}
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (a *BrowserAPI) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.RLock()
		settings := *a.Settings
		a.mu.RUnlock()
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var s BrowserSettings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		a.mu.Lock()
		a.Settings = &s
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, s)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
