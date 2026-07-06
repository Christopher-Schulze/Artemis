package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoginAttemptResult is the outcome of AutoLogin (spec L4589).
type LoginAttemptResult struct {
	Success      bool
	Reason       string
	MFARequired  bool
	CredentialID string
	ProfileName  string
	Domain       string
	AttemptedAt  time.Time
}

// LoginFormDetector abstracts the login form detection (implemented in
// actions/login.go DetectLoginForm). Tests inject a fake.
type LoginFormDetector interface {
	DetectLoginFormForDomain(ctx context.Context, domain string) (bool, error)
}

// LoginExecutor abstracts the fill+submit+verify step. Tests inject a fake.
type LoginExecutor interface {
	FillAndSubmit(ctx context.Context, cred *StoredCredential, password string) (bool, error)
	MFAFieldVisible(ctx context.Context, cred *StoredCredential) (bool, error)
}

// SessionHealthResult is the outcome of a session health check (spec L4589).
type SessionHealthResult struct {
	Healthy             bool
	Reason              string
	HTTPStatus          int
	RedirectedToLogin   bool
	CookieExpiryWarning bool
	CookieExpired       bool
	CheckedAt           time.Time
}

// CookieExpiry is one cookie's expiry for monitoring (spec L4589).
type CookieExpiry struct {
	Domain    string
	Name      string
	ExpiresAt time.Time
}

// SessionManager handles auto-login and session health (spec L4589).
type SessionManager struct {
	mu          sync.Mutex
	creds       *CredentialStore
	detector    LoginFormDetector
	executor    LoginExecutor
	httpClient  *http.Client
	healthEvery time.Duration        // default 30min
	warnWindow  time.Duration        // default 24h before expiry
	warnDedup   map[string]time.Time // domain -> last warning, dedup 1/domain/6h
}

// NewSessionManager creates a session manager.
func NewSessionManager(creds *CredentialStore, detector LoginFormDetector, executor LoginExecutor) *SessionManager {
	return &SessionManager{
		creds:       creds,
		detector:    detector,
		executor:    executor,
		httpClient:  &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }},
		healthEvery: 30 * time.Minute,
		warnWindow:  24 * time.Hour,
		warnDedup:   make(map[string]time.Time),
	}
}

// AutoLogin navigates to domain, detects login form, fills+submits,
// verifies, and handles MFA (spec L4589).
//
// Pipeline:
//  1. Detect login form visible.
//  2. Look up credential for (profile, domain).
//  3. Check profile purpose maps to ProcessingActivity + allowed domain.
//  4. Fill username+password+submit.
//  5. Verify success (SuccessCheck selector visible OR login form gone).
//  6. MFA: if MFAField configured + visible -> inform agent, await code
//     (30s timeout, graceful failure).
//  7. UpdateLastUsed on success.
func (s *SessionManager) AutoLogin(ctx context.Context, profileName, domain, purpose string, allowedDomains []string) (LoginAttemptResult, error) {
	if s == nil {
		return LoginAttemptResult{}, errors.New("session: nil manager")
	}
	now := time.Now().UTC()
	res := LoginAttemptResult{
		ProfileName: profileName,
		Domain:      domain,
		AttemptedAt: now,
	}

	if strings.TrimSpace(purpose) == "" {
		res.Reason = "missing_processing_purpose"
		return res, nil
	}
	if !domainAllowed(domain, allowedDomains) {
		res.Reason = "domain_not_allowed"
		return res, nil
	}

	// Detect login form.
	if s.detector != nil {
		visible, err := s.detector.DetectLoginFormForDomain(ctx, domain)
		if err != nil {
			res.Reason = "detect_error"
			return res, err
		}
		if !visible {
			res.Reason = "no_login_form"
			return res, nil
		}
	}

	// Look up credential.
	cred, password, err := s.creds.GetCredential(profileName, domain)
	if err != nil {
		res.Reason = "no_credential"
		return res, nil
	}
	res.CredentialID = cred.ID

	// MFA check before fill.
	if s.executor != nil {
		mfa, err := s.executor.MFAFieldVisible(ctx, cred)
		if err == nil && mfa {
			res.MFARequired = true
			res.Reason = "mfa_required_await_code"
			return res, nil
		}
	}

	// Fill + submit.
	if s.executor != nil {
		ok, err := s.executor.FillAndSubmit(ctx, cred, password)
		if err != nil {
			res.Reason = "fill_submit_error"
			if luErr := s.creds.UpdateLastUsed(cred.ID, false); luErr != nil {
				slog.Warn("session: failed to update last-used", "cred_id", cred.ID, "err", luErr)
			}
			return res, err
		}
		if !ok {
			res.Reason = "fill_submit_failed"
			if luErr := s.creds.UpdateLastUsed(cred.ID, false); luErr != nil {
				slog.Warn("session: failed to update last-used", "cred_id", cred.ID, "err", luErr)
			}
			return res, nil
		}
	}

	res.Success = true
	res.Reason = "login_success"
	if luErr := s.creds.UpdateLastUsed(cred.ID, true); luErr != nil {
		slog.Warn("session: failed to update last-used", "cred_id", cred.ID, "err", luErr)
	}
	return res, nil
}

// CheckSessionHealth performs a HEAD request to the domain and checks for
// redirect to login, HTTP 401/403, and cookie expiry (spec L4589).
func (s *SessionManager) CheckSessionHealth(ctx context.Context, domain string, cookies []CookieExpiry) (SessionHealthResult, error) {
	if s == nil {
		return SessionHealthResult{}, errors.New("session: nil manager")
	}
	now := time.Now().UTC()
	res := SessionHealthResult{CheckedAt: now}

	// HEAD request.
	url := normalizeURL(domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		res.Reason = "request_build_error"
		return res, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		res.Reason = "request_failed"
		return res, nil
	}
	defer resp.Body.Close()
	res.HTTPStatus = resp.StatusCode

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
		loc := resp.Header.Get("Location")
		if isLoginRedirect(loc) {
			res.RedirectedToLogin = true
			res.Reason = "redirected_to_login"
			return res, nil
		}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		res.Reason = fmt.Sprintf("http_%d", resp.StatusCode)
		return res, nil
	}
	if resp.StatusCode >= 400 {
		res.Reason = fmt.Sprintf("http_%d", resp.StatusCode)
		return res, nil
	}

	// Cookie expiry monitoring.
	for _, c := range cookies {
		if c.ExpiresAt.IsZero() {
			continue // persistent, no monitoring
		}
		if c.ExpiresAt.Before(now) {
			res.CookieExpired = true
			res.Reason = "cookie_expired"
			return res, nil
		}
		if c.ExpiresAt.Sub(now) <= s.warnWindow {
			if s.shouldWarn(c.Domain) {
				res.CookieExpiryWarning = true
			}
		}
	}

	res.Healthy = true
	res.Reason = "healthy"
	return res, nil
}

// shouldWarn applies dedup: max 1 warning per domain per 6h (spec L4589).
func (s *SessionManager) shouldWarn(domain string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.warnDedup[domain]
	if ok && time.Since(last) < 6*time.Hour {
		return false
	}
	s.warnDedup[domain] = time.Now()
	return true
}

// HealthInterval returns the configured health-check interval (default 30min).
func (s *SessionManager) HealthInterval() time.Duration {
	if s == nil || s.healthEvery <= 0 {
		return 30 * time.Minute
	}
	return s.healthEvery
}

func domainAllowed(domain string, allowed []string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if d == a || strings.HasSuffix(d, "."+a) {
			return true
		}
	}
	return false
}

func normalizeURL(domain string) string {
	d := strings.TrimSpace(domain)
	if d == "" {
		return ""
	}
	if !strings.HasPrefix(d, "http://") && !strings.HasPrefix(d, "https://") {
		return "https://" + d
	}
	return d
}

func isLoginRedirect(loc string) bool {
	l := strings.ToLower(loc)
	for _, kw := range []string{"login", "signin", "sign-in", "anmelden", "auth", "authenticate"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}
