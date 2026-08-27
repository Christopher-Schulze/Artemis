package actions

import (
	"context"
	"strings"
	"testing"
)

func TestDetectLoginFormClassic(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/login",
		Forms: []LoginForm{
			{
				ActionURL: "https://app.example.com/auth",
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "username", ID: "user", Autocomplete: "username"},
					{Tag: "input", Type: "password", Name: "password", Autocomplete: "current-password"},
					{Tag: "button", Type: "submit", Text: "Login"},
				},
			},
		},
	}
	dec, err := DetectLoginForm(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Found {
		t.Fatalf("expected found, reason=%s", dec.Reason)
	}
	if dec.FormIdx != 0 {
		t.Fatalf("expected form 0, got %d", dec.FormIdx)
	}
	if dec.UsernameFieldIdx != 0 {
		t.Fatalf("expected username idx 0, got %d", dec.UsernameFieldIdx)
	}
	if dec.PasswordFieldIdx != 1 {
		t.Fatalf("expected password idx 1, got %d", dec.PasswordFieldIdx)
	}
	if dec.SubmitFieldIdx != 2 {
		t.Fatalf("expected submit idx 2, got %d", dec.SubmitFieldIdx)
	}
	if dec.SubmitButtonIdx != -1 {
		t.Fatalf("expected submit button idx -1 (in-form), got %d", dec.SubmitButtonIdx)
	}
}

func TestDetectLoginFormEmailUsername(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/login",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "email", Name: "email", Autocomplete: "email"},
					{Tag: "input", Type: "password", Name: "pwd"},
					{Tag: "input", Type: "submit", Text: "Sign in"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatalf("expected found, reason=%s", dec.Reason)
	}
	if dec.UsernameFieldIdx != 0 {
		t.Fatalf("expected username idx 0 (email), got %d", dec.UsernameFieldIdx)
	}
}

func TestDetectLoginFormGermanLabels(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/anmelden",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "benutzername", Placeholder: "Benutzername"},
					{Tag: "input", Type: "password", Name: "passwort"},
					{Tag: "button", Type: "submit", Text: "Anmelden"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatalf("expected found for German form, reason=%s", dec.Reason)
	}
}

func TestDetectLoginFormUsernameFromAssociatedLabel(t *testing.T) {
	page := LoginPage{
		Forms: []LoginForm{{
			Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "employee", Label: "Benutzername"},
				{Tag: "input", Type: "password", Name: "secret"},
				{Tag: "button", Type: "submit", Text: "Anmelden"},
			},
		}},
	}
	dec, err := DetectLoginForm(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Found || dec.UsernameFieldIdx != 0 {
		t.Fatalf("label-only username field not detected: %+v", dec)
	}
}

func TestDetectLoginFormSubmitButtonOutsideFields(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/login",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
					{Tag: "input", Type: "password", Name: "password"},
				},
				Buttons: []LoginField{
					{Tag: "button", Type: "submit", Text: "Log in"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatalf("expected found, reason=%s", dec.Reason)
	}
	if dec.SubmitFieldIdx != -1 {
		t.Fatalf("expected submit field idx -1, got %d", dec.SubmitFieldIdx)
	}
	if dec.SubmitButtonIdx != 0 {
		t.Fatalf("expected submit button idx 0, got %d", dec.SubmitButtonIdx)
	}
}

func TestDetectLoginFormNoPasswordNotMatched(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/search",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "q"},
					{Tag: "button", Type: "submit", Text: "Search"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if dec.Found {
		t.Fatal("form without password must not be matched")
	}
	if dec.Reason != "no_form_with_password_username_submit" {
		t.Fatalf("expected no_form_with_password_username_submit, got %s", dec.Reason)
	}
}

func TestDetectLoginFormPasswordButNoUsernameNotMatched(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/login",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "password", Name: "password"},
					{Tag: "button", Type: "submit", Text: "Login"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if dec.Found {
		t.Fatal("form with password but no username must not be matched")
	}
}

func TestDetectLoginFormPasswordButNoSubmitNotMatched(t *testing.T) {
	page := LoginPage{
		URL: "https://app.example.com/login",
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "user"},
					{Tag: "input", Type: "password", Name: "password"},
					{Tag: "button", Type: "button", Text: "Settings"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if dec.Found {
		t.Fatal("form without submit must not be matched")
	}
}

func TestDetectLoginFormPicksFirstMatchingForm(t *testing.T) {
	page := LoginPage{
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "search", Name: "q"},
				},
			},
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
					{Tag: "input", Type: "password", Name: "password"},
					{Tag: "button", Type: "submit", Text: "Login"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatal("expected found")
	}
	if dec.FormIdx != 1 {
		t.Fatalf("expected form 1 (first matching), got %d", dec.FormIdx)
	}
}

func TestDetectLoginFormContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page := LoginPage{
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "user"},
					{Tag: "input", Type: "password", Name: "password"},
					{Tag: "button", Type: "submit", Text: "Login"},
				},
			},
		},
	}
	dec, err := DetectLoginForm(ctx, page)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if dec.Found {
		t.Fatal("must not be found on cancelled ctx")
	}
	if dec.Reason != "context_cancelled" {
		t.Fatalf("expected context_cancelled, got %s", dec.Reason)
	}
}

func TestDetectLoginFormNilFormsErrors(t *testing.T) {
	_, err := DetectLoginForm(context.Background(), LoginPage{})
	if err == nil {
		t.Fatal("nil forms must error")
	}
}

func TestDetectLoginFormAriaLabelUsername(t *testing.T) {
	page := LoginPage{
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", AriaLabel: "Username or email"},
					{Tag: "input", Type: "password", AriaLabel: "Password"},
					{Tag: "button", Type: "submit", Text: "Sign in"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatalf("expected found via aria-label, reason=%s", dec.Reason)
	}
}

func TestDetectLoginFormButtonRoleSubmit(t *testing.T) {
	page := LoginPage{
		Forms: []LoginForm{
			{
				Fields: []LoginField{
					{Tag: "input", Type: "text", Name: "user"},
					{Tag: "input", Type: "password", Name: "password"},
				},
				Buttons: []LoginField{
					{Tag: "div", Role: "button", Text: "Log in"},
				},
			},
		},
	}
	dec, _ := DetectLoginForm(context.Background(), page)
	if !dec.Found {
		t.Fatalf("expected found via role=button, reason=%s", dec.Reason)
	}
	if dec.SubmitButtonIdx != 0 {
		t.Fatalf("expected submit button idx 0, got %d", dec.SubmitButtonIdx)
	}
}

func TestDetectLoginFormIntentClassification(t *testing.T) {
	cases := []struct {
		name       string
		form       LoginForm
		wantFound  bool
		wantIntent LoginIntent
		wantReason LoginDetectionReason
	}{
		{
			name: "current_password_login",
			form: LoginForm{Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "identifier", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "password", Autocomplete: "current-password"},
				{Tag: "button", Type: "submit", Text: "Sign in"},
			}},
			wantFound: true, wantIntent: LoginIntentCurrentPassword, wantReason: LoginReasonLoginFormDetected,
		},
		{
			name: "german_current_password_login",
			form: LoginForm{Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "mitarbeiter", Label: "Benutzername"},
				{Tag: "input", Type: "password", Name: "kennwort", Label: "Passwort"},
				{Tag: "button", Type: "submit", Text: "Anmelden"},
			}},
			wantFound: true, wantIntent: LoginIntentCurrentPassword, wantReason: LoginReasonLoginFormDetected,
		},
		{
			name: "new_password",
			form: LoginForm{Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "password", Autocomplete: "new-password"},
				{Tag: "button", Type: "submit", Text: "Login"},
			}},
			wantIntent: LoginIntentAmbiguous, wantReason: LoginReasonNewPasswordField,
		},
		{
			name: "registration_confirmation_pair",
			form: LoginForm{ActionURL: "/account/register", Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "password", Autocomplete: "new-password"},
				{Tag: "input", Type: "password", Name: "password_confirmation", Autocomplete: "new-password"},
				{Tag: "button", Type: "submit", Text: "Create account"},
			}},
			wantIntent: LoginIntentRegistration, wantReason: LoginReasonRegistrationForm,
		},
		{
			name: "password_reset",
			form: LoginForm{ActionURL: "/password/reset", Fields: []LoginField{
				{Tag: "input", Type: "email", Name: "email"},
				{Tag: "input", Type: "password", Name: "password", Autocomplete: "new-password"},
				{Tag: "button", Type: "submit", Text: "Reset password"},
			}},
			wantIntent: LoginIntentPasswordReset, wantReason: LoginReasonPasswordResetForm,
		},
		{
			name: "password_change",
			form: LoginForm{ActionURL: "/account/security", Fields: []LoginField{
				{Tag: "input", Type: "email", Name: "email"},
				{Tag: "input", Type: "password", Name: "current", Autocomplete: "current-password"},
				{Tag: "input", Type: "password", Name: "new", Autocomplete: "new-password"},
				{Tag: "button", Type: "submit", Text: "Save"},
			}},
			wantIntent: LoginIntentPasswordChange, wantReason: LoginReasonPasswordChangeForm,
		},
		{
			name: "ambiguous_multiple_passwords",
			form: LoginForm{Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "first"},
				{Tag: "input", Type: "password", Name: "second"},
				{Tag: "button", Type: "submit", Text: "Login"},
			}},
			wantIntent: LoginIntentAmbiguous, wantReason: LoginReasonAmbiguousPasswordFields,
		},
		{
			name: "ambiguous_without_current_login_signal",
			form: LoginForm{ActionURL: "/account/continue", Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "password"},
				{Tag: "button", Type: "submit", Text: "Continue"},
			}},
			wantIntent: LoginIntentAmbiguous, wantReason: LoginReasonMissingCurrentLoginSignal,
		},
		{
			name: "german_registration",
			form: LoginForm{ActionURL: "/konto/erstellen", Fields: []LoginField{
				{Tag: "input", Type: "text", Name: "benutzername", Autocomplete: "username"},
				{Tag: "input", Type: "password", Name: "passwort", Autocomplete: "new-password"},
				{Tag: "button", Type: "submit", Text: "Konto erstellen"},
			}},
			wantIntent: LoginIntentRegistration, wantReason: LoginReasonRegistrationForm,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dec, err := DetectLoginForm(context.Background(), LoginPage{Forms: []LoginForm{c.form}})
			if err != nil {
				t.Fatal(err)
			}
			if dec.Found != c.wantFound || dec.Intent != c.wantIntent || dec.Reason != c.wantReason {
				t.Fatalf("detection=%+v, want found=%t intent=%q reason=%q", dec, c.wantFound, c.wantIntent, c.wantReason)
			}
			if !c.wantFound && (dec.FormIdx != -1 || dec.UsernameFieldIdx != -1 || dec.PasswordFieldIdx != -1 || dec.SubmitFieldIdx != -1 || dec.SubmitButtonIdx != -1) {
				t.Fatalf("denied detection exposed executable indexes: %+v", dec)
			}
		})
	}
}

func TestDetectLoginFormSkipsDeniedFormForLaterCurrentPasswordLogin(t *testing.T) {
	page := LoginPage{Forms: []LoginForm{
		{ActionURL: "/register", Fields: []LoginField{
			{Tag: "input", Type: "text", Name: "user", Autocomplete: "username"},
			{Tag: "input", Type: "password", Name: "password", Autocomplete: "new-password"},
			{Tag: "button", Type: "submit", Text: "Create account"},
		}},
		{ActionURL: "/login", Fields: []LoginField{
			{Tag: "input", Type: "email", Name: "email"},
			{Tag: "input", Type: "password", Name: "password", Autocomplete: "current-password"},
			{Tag: "button", Type: "submit", Text: "Sign in"},
		}},
	}}
	dec, err := DetectLoginForm(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Found || dec.FormIdx != 1 || dec.Intent != LoginIntentCurrentPassword {
		t.Fatalf("valid later login was not selected: %+v", dec)
	}
}

func TestLoginIntentErrorIsStableAndSecretFree(t *testing.T) {
	sentinel := "credential-value-must-not-appear"
	err := (&LoginIntentError{Intent: LoginIntentRegistration, Reason: LoginReasonRegistrationForm})
	if !strings.Contains(err.Error(), string(LoginReasonRegistrationForm)) {
		t.Fatalf("error=%q does not contain stable reason", err.Error())
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error leaked secret: %q", err.Error())
	}
}

func TestVerifyPostLoginSuccess(t *testing.T) {
	ok := VerifyPostLogin(PostLoginPage{
		URL:            "https://app.example.com/dashboard",
		LogoutSignals:  []string{"Log out", "Account: alice"},
		LoginFormStill: false,
	})
	if !ok {
		t.Fatal("expected post-login verified when form gone + signal present")
	}
}

func TestVerifyPostLoginFormStillVisibleFails(t *testing.T) {
	ok := VerifyPostLogin(PostLoginPage{
		LogoutSignals:  []string{"Log out"},
		LoginFormStill: true,
	})
	if ok {
		t.Fatal("must fail when login form still visible")
	}
}

func TestVerifyPostLoginNoSignalsFails(t *testing.T) {
	ok := VerifyPostLogin(PostLoginPage{
		LogoutSignals: []string{"", "  "},
	})
	if ok {
		t.Fatal("must fail when no real logout signal present")
	}
}
