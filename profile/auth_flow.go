package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/security"
)

type AuthenticationMode string

const (
	AuthModeProvidedCredentials AuthenticationMode = "provided_credentials"
	AuthModeExistingProfile     AuthenticationMode = "existing_profile"
	AuthModeExternalInteractive AuthenticationMode = "external_interactive"
	AuthModeWebBotAuth          AuthenticationMode = "webbot_auth"
	AuthModeMFAContinuation     AuthenticationMode = "mfa_continuation"
)

type AuthenticationStatus string

const (
	AuthStatusAuthenticated AuthenticationStatus = "authenticated"
	AuthStatusMFARequired   AuthenticationStatus = "mfa_required"
	AuthStatusCancelled     AuthenticationStatus = "cancelled"
	AuthStatusDenied        AuthenticationStatus = "denied"
	AuthStatusUnsupported   AuthenticationStatus = "unsupported"
	AuthStatusFailed        AuthenticationStatus = "failed"
)

type AuthenticationRequest struct {
	ProfileName string
	Domain      string
	Purpose     string
	Mode        AuthenticationMode
}

type AuthenticationPolicy struct {
	AllowedDomains []string
	AllowedModes   []AuthenticationMode
	AllowMFA       bool
	MaxDuration    time.Duration
}

type MFARequest struct {
	HandoffID   string
	ProfileName string
	Domain      string
	Methods     []string
	ExpiresAt   time.Time
}

type MFAResponse struct {
	Code      string
	Cancelled bool
}

type MFAHandoff interface {
	RequestCode(context.Context, MFARequest) (MFAResponse, error)
}

type AuthEvidence struct {
	LoginFormGone        bool     `json:"login_form_gone"`
	AuthenticatedSignals []string `json:"authenticated_signals,omitempty"`
	CookieNames          []string `json:"cookie_names,omitempty"`
	ProtectedURL         bool     `json:"protected_url"`
	CallerAssertion      bool     `json:"caller_assertion"`
}

func (e AuthEvidence) Verified() bool {
	if !e.LoginFormGone {
		return false
	}
	return len(e.AuthenticatedSignals) > 0 || len(e.CookieNames) > 0 || e.CallerAssertion
}

type AuthenticationOutcome struct {
	Status       AuthenticationStatus `json:"status"`
	Reason       string               `json:"reason"`
	ProfileName  string               `json:"profile_name,omitempty"`
	Domain       string               `json:"domain,omitempty"`
	CredentialID string               `json:"credential_id,omitempty"`
	Handoff      *MFARequest          `json:"handoff,omitempty"`
	Evidence     AuthEvidence         `json:"evidence"`
}

type SecureLoginExecutor interface {
	DetectLogin(ctx context.Context, domain string) (bool, error)
	FillAndSubmit(ctx context.Context, record *StoredCredential, password func(func(string) error) error) (bool, error)
	MFAFieldVisible(ctx context.Context, record *StoredCredential) (bool, error)
	SubmitMFA(ctx context.Context, code string) error
	VerifyAuthenticated(ctx context.Context, record *StoredCredential) (AuthEvidence, error)
}

// WebBotAuthExecutor is an explicit adapter for providers that own their own
// interactive handoff. Generic credential filling is never substituted for
// this mode when the provider is not present.
type WebBotAuthExecutor interface {
	AuthenticateWebBotAuth(context.Context, AuthenticationRequest) (AuthEvidence, error)
}

type Authenticator struct {
	Store    *CredentialStore
	Executor SecureLoginExecutor
	Handoff  MFAHandoff
	Policy   AuthenticationPolicy
}

func (a *Authenticator) Authenticate(ctx context.Context, request AuthenticationRequest) (AuthenticationOutcome, error) {
	if ctx == nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "context_required"}, errors.New("authentication: context required")
	}
	if err := ctx.Err(); err != nil {
		return AuthenticationOutcome{Status: AuthStatusCancelled, Reason: "cancelled"}, err
	}
	if err := a.validateRequest(request); err != nil {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: stableAuthReason(err), ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	operationCtx := ctx
	if a.Policy.MaxDuration > 0 {
		var cancel context.CancelFunc
		operationCtx, cancel = context.WithTimeout(ctx, a.Policy.MaxDuration)
		defer cancel()
	}
	if a.Executor == nil {
		return AuthenticationOutcome{Status: AuthStatusUnsupported, Reason: "authentication_executor_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	if request.Mode == AuthModeMFAContinuation && !a.Policy.AllowMFA {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: "mfa_disallowed", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	if request.Mode == AuthModeWebBotAuth {
		webBotAuth, ok := a.Executor.(WebBotAuthExecutor)
		if !ok {
			return AuthenticationOutcome{Status: AuthStatusUnsupported, Reason: "webbot_auth_executor_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, nil
		}
		evidence, err := webBotAuth.AuthenticateWebBotAuth(operationCtx, request)
		if err != nil {
			return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "webbot_auth_error", ProfileName: request.ProfileName, Domain: request.Domain}, safeAuthError(err)
		}
		if !evidence.Verified() {
			return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "authenticated_postcondition_missing", ProfileName: request.ProfileName, Domain: request.Domain, Evidence: evidence}, nil
		}
		return AuthenticationOutcome{Status: AuthStatusAuthenticated, Reason: "authenticated_postcondition", ProfileName: request.ProfileName, Domain: request.Domain, Evidence: evidence}, nil
	}
	if request.Mode == AuthModeExistingProfile || request.Mode == AuthModeExternalInteractive {
		evidence, err := a.Executor.VerifyAuthenticated(operationCtx, nil)
		if err != nil {
			return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "verification_error", ProfileName: request.ProfileName, Domain: request.Domain}, safeAuthError(err)
		}
		if evidence.Verified() {
			return AuthenticationOutcome{Status: AuthStatusAuthenticated, Reason: "authenticated_postcondition", ProfileName: request.ProfileName, Domain: request.Domain, Evidence: evidence}, nil
		}
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "authenticated_postcondition_missing", ProfileName: request.ProfileName, Domain: request.Domain, Evidence: evidence}, nil
	}
	if a.Store == nil {
		return AuthenticationOutcome{Status: AuthStatusUnsupported, Reason: "credential_store_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	visible, err := a.Executor.DetectLogin(operationCtx, request.Domain)
	if err != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "login_detection_error", ProfileName: request.ProfileName, Domain: request.Domain}, safeAuthError(err)
	}
	if !visible {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "login_form_missing", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	lease, err := a.Store.AcquireCredential(request.ProfileName, request.Domain)
	if err != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	defer lease.Close()
	record := lease.Record()
	if record == nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_lease_closed", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	if mfa, detectErr := a.Executor.MFAFieldVisible(operationCtx, record); detectErr != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "mfa_detection_error", ProfileName: request.ProfileName, Domain: request.Domain}, safeAuthError(detectErr)
	} else if mfa {
		return a.mfaOutcome(operationCtx, request, record)
	}
	ok, err := a.Executor.FillAndSubmit(operationCtx, record, lease.WithPassword)
	if err != nil || !ok {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		if err != nil {
			return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_submit_error", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, safeAuthError(err)
		}
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_submit_failed", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, nil
	}
	evidence, err := a.Executor.VerifyAuthenticated(operationCtx, record)
	if err != nil {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "verification_error", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, safeAuthError(err)
	}
	if !evidence.Verified() {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "authenticated_postcondition_missing", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, nil
	}
	if err := a.Store.UpdateLastUsed(record.ID, true); err != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_state_update_failed", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, safeAuthError(err)
	}
	return AuthenticationOutcome{Status: AuthStatusAuthenticated, Reason: "authenticated_postcondition", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, nil
}

func (a *Authenticator) mfaOutcome(ctx context.Context, request AuthenticationRequest, record *StoredCredential) (AuthenticationOutcome, error) {
	if !a.Policy.AllowMFA {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: "mfa_disallowed", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, nil
	}
	handoff := &MFARequest{HandoffID: handoffID(record.ID, request.Domain), ProfileName: request.ProfileName, Domain: request.Domain, Methods: []string{"user_code"}, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	if a.Handoff == nil {
		return AuthenticationOutcome{Status: AuthStatusMFARequired, Reason: "mfa_handoff_required", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Handoff: handoff}, nil
	}
	response, err := a.Handoff.RequestCode(ctx, *handoff)
	if err != nil {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "mfa_handoff_error", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Handoff: handoff}, safeAuthError(err)
	}
	if response.Cancelled || strings.TrimSpace(response.Code) == "" {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		return AuthenticationOutcome{Status: AuthStatusCancelled, Reason: "mfa_cancelled", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Handoff: handoff}, nil
	}
	if submitErr := a.Executor.SubmitMFA(ctx, response.Code); submitErr != nil {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "mfa_submit_error", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, safeAuthError(submitErr)
	}
	evidence, err := a.Executor.VerifyAuthenticated(ctx, record)
	if err != nil || !evidence.Verified() {
		_ = a.Store.UpdateLastUsed(record.ID, false)
		if err != nil {
			return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "mfa_verification_error", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID}, safeAuthError(err)
		}
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "mfa_authenticated_postcondition_missing", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, nil
	}
	if err := a.Store.UpdateLastUsed(record.ID, true); err != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "credential_state_update_failed", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, safeAuthError(err)
	}
	return AuthenticationOutcome{Status: AuthStatusAuthenticated, Reason: "mfa_authenticated_postcondition", ProfileName: request.ProfileName, Domain: request.Domain, CredentialID: record.ID, Evidence: evidence}, nil
}

func (a *Authenticator) validateRequest(request AuthenticationRequest) error {
	if a == nil {
		return errors.New("authenticator unavailable")
	}
	if strings.TrimSpace(request.ProfileName) == "" || strings.TrimSpace(request.Domain) == "" || strings.TrimSpace(request.Purpose) == "" {
		return errors.New("required authentication fields missing")
	}
	if request.Mode == "" {
		return errors.New("authentication mode missing")
	}
	if len(a.Policy.AllowedModes) == 0 || !containsAuthMode(a.Policy.AllowedModes, request.Mode) {
		return errors.New("authentication mode disallowed")
	}
	if !domainAllowed(request.Domain, a.Policy.AllowedDomains) {
		return errors.New("authentication domain disallowed")
	}
	return nil
}

func containsAuthMode(modes []AuthenticationMode, want AuthenticationMode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

func handoffID(credentialID, domain string) string {
	sum := sha256.Sum256([]byte(credentialID + "\x00" + strings.ToLower(domain)))
	return "mfa_" + hex.EncodeToString(sum[:12])
}

func stableAuthReason(err error) string {
	if err == nil {
		return "authentication_error"
	}
	return "authentication_denied"
}

func safeAuthError(err error) error {
	if err == nil {
		return nil
	}
	return &AuthenticationError{Code: "executor_failure", cause: err}
}

type AuthenticationError struct {
	Code  string
	cause error
}

func (e *AuthenticationError) Error() string { return "authentication: " + e.Code }
func (e *AuthenticationError) Unwrap() error { return e.cause }

// RedactAuthenticationText is the shared boundary helper for caller-owned
// diagnostics. It accepts exact short-lived secrets without returning them.
func RedactAuthenticationText(text string, secrets ...[]byte) string {
	scrubber := security.NewSecretScrubber("")
	defer scrubber.Close()
	for _, secret := range secrets {
		if len(secret) > 0 {
			_ = scrubber.Add(secret)
		}
	}
	return scrubber.Redact(text)
}
