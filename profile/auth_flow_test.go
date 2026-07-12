package profile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type secureAuthExecutor struct {
	password string
	verified bool
	mfa      bool
	code     string
}

func (e *secureAuthExecutor) DetectLogin(context.Context, string) (bool, error) { return true, nil }
func (e *secureAuthExecutor) FillAndSubmit(_ context.Context, _ *StoredCredential, consume func(func(string) error) error) (bool, error) {
	return true, consume(func(password string) error { e.password = password; return nil })
}
func (e *secureAuthExecutor) MFAFieldVisible(context.Context, *StoredCredential) (bool, error) {
	return e.mfa, nil
}
func (e *secureAuthExecutor) SubmitMFA(_ context.Context, code string) error {
	e.code = code
	return nil
}
func (e *secureAuthExecutor) VerifyAuthenticated(context.Context, *StoredCredential) (AuthEvidence, error) {
	return AuthEvidence{LoginFormGone: e.verified, AuthenticatedSignals: []string{"account-menu"}}, nil
}

type cancelHandoff struct{}

func (cancelHandoff) RequestCode(context.Context, MFARequest) (MFAResponse, error) {
	return MFAResponse{Cancelled: true}, nil
}

func newAuthTestStore(t *testing.T) *CredentialStore {
	t.Helper()
	store, err := NewCredentialStore(t.TempDir()+"/credentials.enc", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreCredential("profile", "example.com", "user@example.com", "secret-password", LoginSelectors{}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAuthenticatorRequiresVerifiedPostconditionAndDoesNotSerializeSecret(t *testing.T) {
	store := newAuthTestStore(t)
	executor := &secureAuthExecutor{verified: true}
	auth := &Authenticator{Store: store, Executor: executor, Policy: AuthenticationPolicy{AllowedDomains: []string{"example.com"}, AllowedModes: []AuthenticationMode{AuthModeProvidedCredentials}}}
	outcome, err := auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "test", Mode: AuthModeProvidedCredentials})
	if err != nil || outcome.Status != AuthStatusAuthenticated {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if executor.password != "secret-password" {
		t.Fatalf("executor did not receive credential")
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "secret-password") {
		t.Fatalf("credential leaked in outcome: %s", encoded)
	}
	executor.verified = false
	outcome, err = auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "test", Mode: AuthModeProvidedCredentials})
	if err != nil || outcome.Status == AuthStatusAuthenticated {
		t.Fatalf("unverified login was accepted: %+v err=%v", outcome, err)
	}
}

func TestAuthenticatorMFAUserCancellationIsTyped(t *testing.T) {
	store := newAuthTestStore(t)
	executor := &secureAuthExecutor{mfa: true}
	auth := &Authenticator{Store: store, Executor: executor, Handoff: cancelHandoff{}, Policy: AuthenticationPolicy{AllowedDomains: []string{"example.com"}, AllowedModes: []AuthenticationMode{AuthModeProvidedCredentials}, AllowMFA: true}}
	outcome, err := auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "test", Mode: AuthModeProvidedCredentials})
	if err != nil || outcome.Status != AuthStatusCancelled || outcome.Handoff == nil {
		t.Fatalf("mfa outcome=%+v err=%v", outcome, err)
	}
	if outcome.Handoff.HandoffID == "" || outcome.Handoff.ExpiresAt.IsZero() {
		t.Fatal("mfa handoff lacks bounded identity")
	}
}

func TestAuthenticatorExistingProfileRequiresAuthenticatedEvidence(t *testing.T) {
	store := newAuthTestStore(t)
	executor := &secureAuthExecutor{verified: false}
	auth := &Authenticator{Store: store, Executor: executor, Policy: AuthenticationPolicy{AllowedDomains: []string{"example.com"}, AllowedModes: []AuthenticationMode{AuthModeExistingProfile}}}
	outcome, err := auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "resume", Mode: AuthModeExistingProfile})
	if err != nil || outcome.Status != AuthStatusFailed || outcome.Reason != "authenticated_postcondition_missing" {
		t.Fatalf("unverified profile outcome=%+v err=%v", outcome, err)
	}
	executor.verified = true
	outcome, err = auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "resume", Mode: AuthModeExistingProfile})
	if err != nil || outcome.Status != AuthStatusAuthenticated || !outcome.Evidence.Verified() {
		t.Fatalf("verified profile outcome=%+v err=%v", outcome, err)
	}
}

func TestAuthenticatorDoesNotSubstituteCredentialsForWebBotAuth(t *testing.T) {
	store := newAuthTestStore(t)
	auth := &Authenticator{Store: store, Executor: &secureAuthExecutor{verified: true}, Policy: AuthenticationPolicy{AllowedDomains: []string{"example.com"}, AllowedModes: []AuthenticationMode{AuthModeWebBotAuth}}}
	outcome, err := auth.Authenticate(context.Background(), AuthenticationRequest{ProfileName: "profile", Domain: "example.com", Purpose: "test", Mode: AuthModeWebBotAuth})
	if err != nil || outcome.Status != AuthStatusUnsupported || outcome.Reason != "webbot_auth_executor_unavailable" {
		t.Fatalf("webbot auth outcome=%+v err=%v", outcome, err)
	}
}
