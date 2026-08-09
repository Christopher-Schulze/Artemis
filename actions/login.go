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
	Label        string
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

// LoginIntent identifies the safe interpretation of a credential-bearing
// form. Only LoginIntentCurrentPassword is eligible for stored credentials.
type LoginIntent string

const (
	LoginIntentUnknown         LoginIntent = "unknown"
	LoginIntentCurrentPassword LoginIntent = "current_password_login"
	LoginIntentRegistration    LoginIntent = "registration"
	LoginIntentPasswordReset   LoginIntent = "password_reset"
	LoginIntentPasswordChange  LoginIntent = "password_change"
	LoginIntentAmbiguous       LoginIntent = "ambiguous"
)

// LoginDetectionReason is a stable, secret-free reason for a detection
// decision. Callers can use it for audit evidence without exposing form data.
type LoginDetectionReason string

const (
	LoginReasonUnknown                          LoginDetectionReason = "unknown"
	LoginReasonContextRequired                  LoginDetectionReason = "context_required"
	LoginReasonContextCancelled                 LoginDetectionReason = "context_cancelled"
	LoginReasonLoginFormDetected                LoginDetectionReason = "login_form_detected"
	LoginReasonNoFormWithPasswordUsernameSubmit LoginDetectionReason = "no_form_with_password_username_submit"
	LoginReasonRegistrationForm                 LoginDetectionReason = "registration_form_not_login"
	LoginReasonPasswordResetForm                LoginDetectionReason = "password_reset_form_not_login"
	LoginReasonPasswordChangeForm               LoginDetectionReason = "password_change_form_not_login"
	LoginReasonNewPasswordField                 LoginDetectionReason = "new_password_not_login"
	LoginReasonPasswordConfirmationField        LoginDetectionReason = "password_confirmation_not_login"
	LoginReasonAmbiguousPasswordFields          LoginDetectionReason = "multiple_password_fields_ambiguous"
	LoginReasonMissingCurrentLoginSignal        LoginDetectionReason = "current_login_intent_ambiguous"
)

// LoginIntentError reports a denied form intent without including raw form
// content, field values or credential material.
type LoginIntentError struct {
	Intent LoginIntent
	Reason LoginDetectionReason
}

func (e *LoginIntentError) Error() string {
	if e == nil {
		return "login intent denied"
	}
	reason := e.Reason
	if reason == "" {
		reason = LoginReasonUnknown
	}
	return "login intent denied: " + string(reason)
}

// LoginDetection is the outcome of DetectLoginForm.
type LoginDetection struct {
	Found            bool
	FormIdx          int // index into page.Forms, -1 if not found
	UsernameFieldIdx int // index into form.Fields
	PasswordFieldIdx int // index into form.Fields
	SubmitFieldIdx   int // index into form.Fields (or -1 if submit is in Buttons)
	SubmitButtonIdx  int // index into form.Buttons when SubmitFieldIdx == -1
	Intent           LoginIntent
	Reason           LoginDetectionReason
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
//   - password: input with type=password OR an exact password autocomplete token.
//   - username: input with type=email/text, name/id/autocomplete/label
//     matching a username keyword, OR autocomplete=username.
//   - submit: input type=submit, button type=submit, or button text matching
//     a submit keyword.
//   - intent: exactly one non-new-password password field with no explicit
//     registration, reset, change or confirmation signal and either a
//     current-password autocomplete token or login-specific action/submit
//     signal is a current-password login; every other intent is denied.
//
// Returns the first form that satisfies all structural and intent checks. If
// no form has all of them, returns LoginDetection{Found:false} with a stable,
// secret-free reason.
func DetectLoginForm(ctx context.Context, page LoginPage) (LoginDetection, error) {
	if page.Forms == nil {
		return emptyLoginDetection(LoginReasonUnknown), errors.New("login: page has no forms")
	}
	if ctx == nil {
		return emptyLoginDetection(LoginReasonContextRequired), errors.New("login: context required")
	}
	if err := ctx.Err(); err != nil {
		return emptyLoginDetection(LoginReasonContextCancelled), err
	}

	var denied LoginDetection
	for fi, form := range page.Forms {
		passwordIdxs := findPasswordFields(form.Fields)
		if len(passwordIdxs) == 0 {
			continue
		}
		intent, reason := classifyLoginIntent(form, passwordIdxs)
		if intent != LoginIntentCurrentPassword {
			if denied.Reason == "" {
				denied = emptyLoginDetection(reason)
				denied.Intent = intent
			}
			continue
		}
		pwIdx := passwordIdxs[0]
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
			Intent:           LoginIntentCurrentPassword,
			Reason:           LoginReasonLoginFormDetected,
		}
		return dec, nil
	}
	if denied.Reason != "" {
		return denied, nil
	}
	return emptyLoginDetection(LoginReasonNoFormWithPasswordUsernameSubmit), nil
}

func emptyLoginDetection(reason LoginDetectionReason) LoginDetection {
	return LoginDetection{
		FormIdx:          -1,
		UsernameFieldIdx: -1,
		PasswordFieldIdx: -1,
		SubmitFieldIdx:   -1,
		SubmitButtonIdx:  -1,
		Intent:           LoginIntentUnknown,
		Reason:           reason,
	}
}

func findPasswordFields(fields []LoginField) []int {
	indexes := make([]int, 0, 2)
	for i, f := range fields {
		if isPasswordField(f) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func findPasswordField(fields []LoginField) int {
	indexes := findPasswordFields(fields)
	if len(indexes) > 0 {
		return indexes[0]
	}
	return -1
}

func isPasswordField(f LoginField) bool {
	if strings.EqualFold(strings.TrimSpace(f.Type), "password") {
		return true
	}
	for _, value := range passwordAutocompleteValues {
		if hasAutocompleteToken(f.Autocomplete, value) {
			return true
		}
	}
	return false
}

func hasAutocompleteToken(value, wanted string) bool {
	for _, token := range strings.Fields(lower(value)) {
		if token == wanted {
			return true
		}
	}
	return false
}

func classifyLoginIntent(form LoginForm, passwordIdxs []int) (LoginIntent, LoginDetectionReason) {
	hasCurrentPassword := false
	hasNewPassword := false
	hasConfirmation := false
	for _, index := range passwordIdxs {
		field := form.Fields[index]
		hasCurrentPassword = hasCurrentPassword || hasAutocompleteToken(field.Autocomplete, "current-password")
		hasNewPassword = hasNewPassword || hasAutocompleteToken(field.Autocomplete, "new-password")
		fieldText := loginFieldIntentText(field)
		hasNewPassword = hasNewPassword || containsIntentPhrase(fieldText, newPasswordPhrases)
		hasConfirmation = hasConfirmation || containsIntentPhrase(fieldText, passwordConfirmationPhrases)
	}

	switch explicitIntent := explicitFormIntent(form, passwordIdxs); explicitIntent {
	case LoginIntentPasswordChange:
		return explicitIntent, LoginReasonPasswordChangeForm
	case LoginIntentPasswordReset:
		return explicitIntent, LoginReasonPasswordResetForm
	case LoginIntentRegistration:
		return explicitIntent, LoginReasonRegistrationForm
	}
	if hasCurrentPassword && hasNewPassword {
		return LoginIntentPasswordChange, LoginReasonPasswordChangeForm
	}
	if hasConfirmation {
		return LoginIntentAmbiguous, LoginReasonPasswordConfirmationField
	}
	if len(passwordIdxs) > 1 {
		return LoginIntentAmbiguous, LoginReasonAmbiguousPasswordFields
	}
	if hasNewPassword {
		return LoginIntentAmbiguous, LoginReasonNewPasswordField
	}
	if !hasCurrentPassword && !hasPositiveLoginSignal(form) {
		return LoginIntentAmbiguous, LoginReasonMissingCurrentLoginSignal
	}
	return LoginIntentCurrentPassword, LoginReasonLoginFormDetected
}

func explicitFormIntent(form LoginForm, passwordIdxs []int) LoginIntent {
	texts := []string{form.ActionURL}
	for _, index := range passwordIdxs {
		texts = append(texts, loginFieldIntentText(form.Fields[index]))
	}
	for _, field := range form.Fields {
		if isIntentSubmitControl(field) {
			texts = append(texts, loginFieldIntentText(field), field.Text)
		}
	}
	for _, button := range form.Buttons {
		if isIntentSubmitControl(button) {
			texts = append(texts, loginFieldIntentText(button), button.Text)
		}
	}

	for _, text := range texts {
		if intentFromText(text) == LoginIntentPasswordChange {
			return LoginIntentPasswordChange
		}
	}
	for _, text := range texts {
		if intentFromText(text) == LoginIntentPasswordReset {
			return LoginIntentPasswordReset
		}
	}
	for _, text := range texts {
		if intentFromText(text) == LoginIntentRegistration {
			return LoginIntentRegistration
		}
	}
	return LoginIntentUnknown
}

func isIntentSubmitControl(field LoginField) bool {
	fieldType := strings.TrimSpace(field.Type)
	if strings.EqualFold(fieldType, "submit") || strings.EqualFold(strings.TrimSpace(field.Role), "submit") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(field.Tag), "button") &&
		(fieldType == "" || strings.EqualFold(fieldType, "submit"))
}

func hasPositiveLoginSignal(form LoginForm) bool {
	texts := []string{form.ActionURL}
	for _, field := range form.Fields {
		if isIntentSubmitControl(field) || strings.EqualFold(strings.TrimSpace(field.Role), "button") {
			texts = append(texts, field.Text, field.Name, field.ID, field.Label, field.AriaLabel)
		}
	}
	for _, button := range form.Buttons {
		if isIntentSubmitControl(button) || strings.EqualFold(strings.TrimSpace(button.Role), "button") {
			texts = append(texts, button.Text, button.Name, button.ID, button.Label, button.AriaLabel)
		}
	}
	for _, text := range texts {
		if containsIntentPhrase(text, loginSignalPhrases) {
			return true
		}
	}
	return false
}

func loginFieldIntentText(field LoginField) string {
	return strings.Join([]string{field.Name, field.ID, field.Autocomplete, field.Placeholder, field.Label, field.AriaLabel}, " ")
}

func intentFromText(value string) LoginIntent {
	text := normalizeIntentText(value)
	if containsIntentPhrase(text, passwordChangePhrases) {
		return LoginIntentPasswordChange
	}
	if containsIntentPhrase(text, passwordResetPhrases) {
		return LoginIntentPasswordReset
	}
	if containsIntentPhrase(text, registrationPhrases) {
		return LoginIntentRegistration
	}
	return LoginIntentUnknown
}

func containsIntentPhrase(value string, phrases []string) bool {
	text := " " + normalizeIntentText(value) + " "
	for _, phrase := range phrases {
		wanted := " " + normalizeIntentText(phrase) + " "
		if strings.Contains(text, wanted) {
			return true
		}
	}
	return false
}

func normalizeIntentText(value string) string {
	value = lower(value)
	value = strings.NewReplacer("-", " ", "_", " ", "/", " ", ".", " ", ":", " ", "?", " ", "=", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

var registrationPhrases = []string{
	"register", "registration", "registrieren", "registrierung", "sign up", "signup",
	"create account", "new account", "konto erstellen", "konto anlegen",
}

var passwordResetPhrases = []string{
	"reset", "reset password", "forgot password", "forgotten password", "password recovery",
	"recover password", "passwort vergessen", "kennwort vergessen", "passwort zurücksetzen",
	"passwort zurucksetzen", "kennwort zurücksetzen", "kennwort zurucksetzen",
}

var passwordChangePhrases = []string{
	"change password", "change your password", "password change", "update password",
	"passwort ändern", "passwort aendern", "kennwort ändern", "kennwort aendern", "passwortwechsel",
}

var newPasswordPhrases = []string{
	"new password", "neues passwort", "neues kennwort",
}

var passwordConfirmationPhrases = []string{
	"confirm password", "password confirmation", "repeat password", "retype password",
	"password repeat", "passwort bestätigen", "passwort bestaetigen", "passwort wiederholen",
	"kennwort bestätigen", "kennwort bestaetigen", "kennwort wiederholen",
}

var loginSignalPhrases = []string{
	"login", "sign in", "signin", "log in", "anmelden", "anmeldung", "einloggen",
	"authenticate", "authentication", "auth",
}

func findUsernameField(fields []LoginField) int {
	for i, f := range fields {
		if !isUsernameCandidate(f) {
			continue
		}
		// Skip the password field itself, including autocomplete-only fields.
		if isPasswordField(f) {
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
	combined := lower(f.Name + " " + f.ID + " " + f.Placeholder + " " + f.Label + " " + f.AriaLabel)
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
