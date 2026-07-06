package profile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDetector implements LoginFormDetector.
type fakeDetector struct {
	visible bool
	err     error
}

func (f *fakeDetector) DetectLoginFormForDomain(ctx context.Context, domain string) (bool, error) {
	return f.visible, f.err
}

// fakeExecutor implements LoginExecutor.
type fakeExecutor struct {
	fillOK     bool
	fillErr    error
	mfaVisible bool
	mfaErr     error
}

func (f *fakeExecutor) FillAndSubmit(ctx context.Context, cred *StoredCredential, password string) (bool, error) {
	return f.fillOK, f.fillErr
}

func (f *fakeExecutor) MFAFieldVisible(ctx context.Context, cred *StoredCredential) (bool, error) {
	return f.mfaVisible, f.mfaErr
}

func newTestSessionManager(t *testing.T) (*SessionManager, *CredentialStore) {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/credentials.enc"
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	creds, err := NewCredentialStore(path, key)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}
	return NewSessionManager(creds, nil, nil), creds
}

func TestAutoLogin_NoPurpose_Blocked(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.AutoLogin(context.Background(), "p", "turbomed.de", "", []string{"turbomed.de"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Success {
		t.Fatal("should not succeed without purpose")
	}
	if res.Reason != "missing_processing_purpose" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestAutoLogin_DomainNotAllowed(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "evil.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := s.AutoLogin(context.Background(), "p", "evil.de", "purpose1", []string{"turbomed.de"})
	if res.Success {
		t.Fatal("should not succeed for non-allowed domain")
	}
	if res.Reason != "domain_not_allowed" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestAutoLogin_NoLoginFormVisible(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	s.detector = &fakeDetector{visible: false}
	res, _ := s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if res.Success {
		t.Fatal("should not succeed when no login form visible")
	}
	if res.Reason != "no_login_form" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestAutoLogin_NoCredential(t *testing.T) {
	s, _ := newTestSessionManager(t)
	s.detector = &fakeDetector{visible: true}
	res, _ := s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if res.Success {
		t.Fatal("should not succeed without credential")
	}
	if res.Reason != "no_credential" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestAutoLogin_MFARequired(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{MFAField: "#mfa"})
	if err != nil {
		t.Fatal(err)
	}
	s.detector = &fakeDetector{visible: true}
	s.executor = &fakeExecutor{mfaVisible: true}
	res, _ := s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if res.Success {
		t.Fatal("should not succeed when MFA required")
	}
	if !res.MFARequired {
		t.Fatal("MFARequired not set")
	}
	if res.Reason != "mfa_required_await_code" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestAutoLogin_Success(t *testing.T) {
	s, creds := newTestSessionManager(t)
	id, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	s.detector = &fakeDetector{visible: true}
	s.executor = &fakeExecutor{fillOK: true}
	res, _ := s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if !res.Success {
		t.Fatalf("expected success, reason=%s", res.Reason)
	}
	if res.CredentialID != id {
		t.Fatalf("credential id mismatch: %s vs %s", res.CredentialID, id)
	}
	// LastLoginOK should be true after success
	rec, _, _ := creds.GetCredential("p", "turbomed.de")
	if !rec.LastLoginOK {
		t.Fatal("LastLoginOK not set after success")
	}
}

func TestAutoLogin_FillSubmitFailure(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	s.detector = &fakeDetector{visible: true}
	s.executor = &fakeExecutor{fillOK: false}
	res, _ := s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if res.Success {
		t.Fatal("should not succeed on fill failure")
	}
	if res.Reason != "fill_submit_failed" {
		t.Fatalf("reason=%s", res.Reason)
	}
	rec, _, _ := creds.GetCredential("p", "turbomed.de")
	if rec.LastLoginOK {
		t.Fatal("LastLoginOK should be false on failure")
	}
}

func TestAutoLogin_FillSubmitError(t *testing.T) {
	s, creds := newTestSessionManager(t)
	_, err := creds.StoreCredential("p", "turbomed.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	s.detector = &fakeDetector{visible: true}
	s.executor = &fakeExecutor{fillErr: errors.New("network")}
	_, err = s.AutoLogin(context.Background(), "p", "turbomed.de", "purpose1", []string{"turbomed.de"})
	if err == nil {
		t.Fatal("expected error from fill")
	}
}

func TestCheckSessionHealth_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL

	res, err := s.CheckSessionHealth(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Healthy {
		t.Fatalf("expected healthy, reason=%s http=%d", res.Reason, res.HTTPStatus)
	}
}

func TestCheckSessionHealth_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL
	res, _ := s.CheckSessionHealth(context.Background(), url, nil)
	if res.Healthy {
		t.Fatal("should not be healthy on 401")
	}
	if !strings.Contains(res.Reason, "401") {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestCheckSessionHealth_RedirectToLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/login")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL
	res, _ := s.CheckSessionHealth(context.Background(), url, nil)
	if res.Healthy {
		t.Fatal("should not be healthy on login redirect")
	}
	if !res.RedirectedToLogin {
		t.Fatal("RedirectedToLogin not set")
	}
}

func TestCheckSessionHealth_RedirectNotLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/dashboard")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL
	res, _ := s.CheckSessionHealth(context.Background(), url, nil)
	// Redirect to non-login -> still considered healthy (no login redirect, no error status)
	if res.RedirectedToLogin {
		t.Fatal("should not flag non-login redirect")
	}
}

func TestCheckSessionHealth_CookieExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL
	cookies := []CookieExpiry{
		{Domain: url, Name: "sess", ExpiresAt: time.Now().Add(-1 * time.Hour)},
	}
	res, _ := s.CheckSessionHealth(context.Background(), url, cookies)
	if res.Healthy {
		t.Fatal("should not be healthy with expired cookie")
	}
	if !res.CookieExpired {
		t.Fatal("CookieExpired not set")
	}
}

func TestCheckSessionHealth_CookieExpiryWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	s, _ := newTestSessionManager(t)
	url := srv.URL
	cookies := []CookieExpiry{
		{Domain: url, Name: "sess", ExpiresAt: time.Now().Add(2 * time.Hour)},
	}
	res, _ := s.CheckSessionHealth(context.Background(), url, cookies)
	if !res.Healthy {
		t.Fatalf("should be healthy with warning, reason=%s", res.Reason)
	}
	if !res.CookieExpiryWarning {
		t.Fatal("CookieExpiryWarning not set")
	}
	// Dedup: second call within 6h should NOT warn
	res2, _ := s.CheckSessionHealth(context.Background(), url, cookies)
	if res2.CookieExpiryWarning {
		t.Fatal("dedup failed: warned twice")
	}
}

func TestCheckSessionHealth_RequestFailed(t *testing.T) {
	s, _ := newTestSessionManager(t)
	// Use a non-routable address to force failure
	res, err := s.CheckSessionHealth(context.Background(), "http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Healthy {
		t.Fatal("should not be healthy on request failure")
	}
	if res.Reason != "request_failed" {
		t.Fatalf("reason=%s", res.Reason)
	}
}

func TestDomainAllowed(t *testing.T) {
	allowed := []string{"turbomed.de", "example.com"}
	if !domainAllowed("turbomed.de", allowed) {
		t.Fatal("exact match failed")
	}
	if !domainAllowed("app.turbomed.de", allowed) {
		t.Fatal("subdomain match failed")
	}
	if domainAllowed("evil.de", allowed) {
		t.Fatal("non-allowed domain passed")
	}
	if domainAllowed("notturbomed.de", allowed) {
		t.Fatal("suffix-only domain passed")
	}
}

func TestIsLoginRedirect(t *testing.T) {
	cases := []struct {
		loc  string
		want bool
	}{
		{"/login", true},
		{"/auth/signin", true},
		{"/anmelden", true},
		{"/dashboard", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoginRedirect(c.loc); got != c.want {
			t.Fatalf("isLoginRedirect(%q)=%v want %v", c.loc, got, c.want)
		}
	}
}

func TestSessionManager_HealthInterval(t *testing.T) {
	s, _ := newTestSessionManager(t)
	if d := s.HealthInterval(); d != 30*time.Minute {
		t.Fatalf("HealthInterval=%v want 30m", d)
	}
}
