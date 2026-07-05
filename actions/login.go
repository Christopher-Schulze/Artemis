package actions

import (
	"context"
	"errors"
	"strings"
)

// LoginField is a minimal DOM input projection for login form detection.
// Real callers adapt their DOM/AX tree into this shape; the heuristic is
// driver-agnostic so it is unit-testable without a live browser.
type LoginField struct {
	Tag          string
	Type         string // input type attribute
	Name         string
	ID           string
	Autocomplete string
	Placeholder  string
	AriaLabel    string
	Text         string // for button elements
	Role         string // ARIA role
}

// LoginForm is a form projection consumed by DetectLoginForm.
type LoginForm struct {
	ActionURL string
	Fields    []LoginField
	Buttons   []LoginField // submit candidates outside form.Fields
}

// LoginPage is the page projection.
type LoginPage struct {
	URL   string
	Forms []LoginForm
}

// LoginDetection is the outcome of DetectLoginForm.
type LoginDetection struct {
	Found            bool
	FormIdx          int // index into page.Forms, -1 if not found
	UsernameFieldIdx int // index into form.Fields
	PasswordFieldIdx int // index into form.Fields
	SubmitFieldIdx   int // index into form.Fields (or -1 if submit is in Buttons)
	SubmitButtonIdx  int // index into form.Buttons when SubmitFieldIdx == -1
	Reason           string
}

// usernameKeywords match username field name/id/autocomplete/label/aria.
var usernameKeywords = []string{
	"user", "login", "email", "account", "benutzer", "benutzername",
	"nutzer", "nutzername", "anmeldename", "mitarbeiternummer",
}

// passwordAutocomplete values that mark a password field.
var passwordAutocompleteValues = []string{
	"current-password", "new-password", "current", "password",
}

// submitKeywords match submit button text (spec L4555).
var submitKeywords = []string{
	"login", "sign in", "signin", "log in", "submit", "anmelden",
	"anmeldung", "weiter", "continue", "go", "ok",
}

// DetectLoginForm finds a form with a password-type input, a username field,
// and a submit button (spec L4555).
//
// Detection rules:
//   - password: input with type=password OR autocomplete contains "password".
//   - username: input with type=email/text, name/id/autocomplete/label
//     matching a username keyword, OR autocomplete=username.
//   - submit: input type=submit, button type=submit, or button text matching
//     a submit keyword.
//
// Returns the first form that satisfies all three. If no form has all three,
// returns LoginDetection{Found:false} with a reason.
func DetectLoginForm(ctx context.Context, page LoginPage) (LoginDetection, error) {
	if page.Forms == nil {
		return LoginDetection{FormIdx: -1, UsernameFieldIdx: -1, PasswordFieldIdx: -1, SubmitFieldIdx: -1, SubmitButtonIdx: -1}, errors.New("login: page has no forms")
	}
	if ctx.Err() != nil {
		return LoginDetection{FormIdx: -1, UsernameFieldIdx: -1, PasswordFieldIdx: -1, SubmitFieldIdx: -1, SubmitButtonIdx: -1, Reason: "context_cancelled"}, ctx.Err()
	}

	for fi, form := range page.Forms {
		pwIdx := findPasswordField(form.Fields)
		if pwIdx < 0 {
			continue
		}
		userIdx := findUsernameField(form.Fields)
		if userIdx < 0 {
			continue
		}
		submitInForm, submitInButtons := findSubmit(form)
		if submitInForm < 0 && submitInButtons < 0 {
			continue
		}
		dec := LoginDetection{
			Found:            true,
			FormIdx:          fi,
			UsernameFieldIdx: userIdx,
			PasswordFieldIdx: pwIdx,
			SubmitFieldIdx:   submitInForm,
			SubmitButtonIdx:  submitInButtons,
			Reason:           "login_form_detected",
		}
		return dec, nil
	}
	return LoginDetection{
		FormIdx:          -1,
		UsernameFieldIdx: -1,
		PasswordFieldIdx: -1,
		SubmitFieldIdx:   -1,
		SubmitButtonIdx:  -1,
		Reason:           "no_form_with_password_username_submit",
	}, nil
}

func findPasswordField(fields []LoginField) int {
	for i, f := range fields {
		if strings.EqualFold(f.Type, "password") {
			return i
		}
		if containsAny(lower(f.Autocomplete), passwordAutocompleteValues) {
			return i
		}
	}
	return -1
}

func findUsernameField(fields []LoginField) int {
	for i, f := range fields {
		if !isUsernameCandidate(f) {
			continue
		}
		// Skip the password field itself.
		if strings.EqualFold(f.Type, "password") {
			continue
		}
		return i
	}
	return -1
}

func isUsernameCandidate(f LoginField) bool {
	if strings.EqualFold(f.Autocomplete, "username") {
		return true
	}
	t := lower(f.Type)
	if t != "email" && t != "text" && t != "" {
		return false
	}
	combined := lower(f.Name + " " + f.ID + " " + f.Placeholder + " " + f.AriaLabel)
	if containsAny(combined, usernameKeywords) {
		return true
	}
	if t == "email" {
		return true
	}
	return false
}

func findSubmit(form LoginForm) (inFormIdx, inButtonsIdx int) {
	inFormIdx = -1
	inButtonsIdx = -1
	for i, f := range form.Fields {
		if isSubmitCandidate(f) {
			inFormIdx = i
			return
		}
	}
	for i, b := range form.Buttons {
		if isSubmitCandidate(b) {
			inButtonsIdx = i
			return
		}
	}
	return
}

func isSubmitCandidate(f LoginField) bool {
	if strings.EqualFold(f.Type, "submit") {
		return true
	}
	if strings.EqualFold(f.Role, "submit") {
		return true
	}
	txt := lower(strings.TrimSpace(f.Text))
	if txt == "" {
		return false
	}
	if strings.EqualFold(f.Tag, "button") && (f.Type == "" || strings.EqualFold(f.Type, "submit")) {
		if containsAny(txt, submitKeywords) {
			return true
		}
	}
	if strings.EqualFold(f.Role, "button") && containsAny(txt, submitKeywords) {
		return true
	}
	return false
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func lower(s string) string { return strings.ToLower(s) }

// VerifyPostLogin checks post-login persistence signals (spec L4555).
// Returns true when the login form is no longer visible AND a logged-in
// signal is present (e.g. logout link, user avatar, account text).
type PostLoginPage struct {
	URL            string
	LoginFormStill bool
	LogoutSignals  []string // texts/aria-labels indicating logged-in state
}

// VerifyPostLogin returns true when the login form disappeared and at least
// one logged-in signal is present.
func VerifyPostLogin(page PostLoginPage) bool {
	if page.LoginFormStill {
		return false
	}
	for _, s := range page.LogoutSignals {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}
