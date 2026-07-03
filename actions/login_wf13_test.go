package actions

import (
	"context"
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-actions-SEC (login.go, security_privacy)
// Claim: DetectLoginForm denies incomplete forms and VerifyPostLogin denies
// when the login form is still visible
// =============================================================================

func TestWFArtemisActions_LoginDetectionDeniesIncompleteForms(t *testing.T) {
	// Security: login form detection must deny incomplete forms (missing
	// password, username, or submit) to prevent credential injection into
	// non-login forms. Post-login verification must deny when the login
	// form is still visible.

	cases := []struct {
		name   string
		page   LoginPage
		reason string
	}{
		{
			"no_password_field",
			LoginPage{URL: "https://example.com/login", Forms: []LoginForm{
				{ActionURL: "/login", Fields: []LoginField{
					{Tag: "input", Type: "email", Name: "user", Autocomplete: "username"},
					{Tag: "button", Type: "submit", Text: "Login"},
				}},
			}},
			"no_form_with_password_username_submit",
		},
		{
			"no_username_field",
			LoginPage{URL: "https://example.com/login", Forms: []LoginForm{
				{ActionURL: "/login", Fields: []LoginField{
					{Tag: "input", Type: "password", Name: "pwd"},
					{Tag: "button", Type: "submit", Text: "Login"},
				}},
			}},
			"no_form_with_password_username_submit",
		},
		{
			"no_submit_button",
			LoginPage{URL: "https://example.com/login", Forms: []LoginForm{
				{ActionURL: "/login", Fields: []LoginField{
					{Tag: "input", Type: "email", Name: "user", Autocomplete: "username"},
					{Tag: "input", Type: "password", Name: "pwd"},
				}},
			}},
			"no_form_with_password_username_submit",
		},
		{
			"nil_forms",
			LoginPage{URL: "https://example.com/login", Forms: nil},
			"",
		},
	}
	blocked := 0
	for _, c := range cases {
		dec, err := DetectLoginForm(context.Background(), c.page)
		if c.reason == "" {
			// nil forms case: expect error
			if err == nil {
				t.Fatalf("%s: expected error for nil forms, got nil", c.name)
			}
		} else {
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", c.name, err)
			}
			if dec.Found {
				t.Fatalf("%s: expected Found=false, got true", c.name)
			}
			if dec.Reason != c.reason {
				t.Fatalf("%s: expected reason %q, got %q", c.name, c.reason, dec.Reason)
			}
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all incomplete forms denied), got %.1f", denyRate)
	}

	// Post-login deny: login form still visible
	if VerifyPostLogin(PostLoginPage{URL: "https://example.com/home", LoginFormStill: true, LogoutSignals: []string{"Logout"}}) {
		t.Fatal("VerifyPostLogin must deny when login form is still visible")
	}
	// Post-login deny: no logout signals
	if VerifyPostLogin(PostLoginPage{URL: "https://example.com/home", LoginFormStill: false, LogoutSignals: nil}) {
		t.Fatal("VerifyPostLogin must deny when no logout signals present")
	}

	// Baseline: complete form is detected (positive control)
	completePage := LoginPage{URL: "https://example.com/login", Forms: []LoginForm{
		{ActionURL: "/login", Fields: []LoginField{
			{Tag: "input", Type: "email", Name: "user", Autocomplete: "username"},
			{Tag: "input", Type: "password", Name: "pwd"},
			{Tag: "button", Type: "submit", Text: "Login"},
		}},
	}}
	dec, err := DetectLoginForm(context.Background(), completePage)
	if err != nil || !dec.Found {
		t.Fatalf("complete form must be detected, got dec=%+v err=%v", dec, err)
	}
	// Baseline: post-login success
	if !VerifyPostLogin(PostLoginPage{URL: "https://example.com/home", LoginFormStill: false, LogoutSignals: []string{"Logout"}}) {
		t.Fatal("VerifyPostLogin must accept when form gone and logout signal present")
	}
}
