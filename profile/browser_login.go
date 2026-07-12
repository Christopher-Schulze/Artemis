package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

// BrowserLoginExecutor adapts the secure authentication contract to one
// owned CDP page. Secret values are sent only in the bounded CDP command and
// are never retained in the executor or returned in evidence.
type BrowserLoginExecutor struct {
	page      *bridge.Page
	selectors LoginSelectors
}

func NewBrowserLoginExecutor(page *bridge.Page, selectors LoginSelectors) (*BrowserLoginExecutor, error) {
	if page == nil {
		return nil, errors.New("browser login executor: page required")
	}
	return &BrowserLoginExecutor{page: page, selectors: selectors}, nil
}

func (e *BrowserLoginExecutor) DetectLogin(ctx context.Context, _ string) (bool, error) {
	var state struct {
		Ready    string `json:"ready"`
		Password bool   `json:"password"`
		User     bool   `json:"user"`
		Submit   bool   `json:"submit"`
	}
	expression := fmt.Sprintf(`(() => ({ready:document.readyState,password:!!document.querySelector(%s),user:!!document.querySelector(%s),submit:!!document.querySelector(%s)}))()`, jsString(firstOr(e.selectors.PasswordField, `input[type="password"]`)), jsString(firstOr(e.selectors.UsernameField, `input[autocomplete="username"],input[type="email"],input[name*="user" i]`)), jsString(firstOr(e.selectors.SubmitButton, `button[type="submit"],input[type="submit"]`)))
	for {
		if err := e.evaluate(ctx, expression, &state); err != nil {
			return false, err
		}
		if state.Ready != "loading" {
			return state.Password && state.User && state.Submit, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (e *BrowserLoginExecutor) FillAndSubmit(ctx context.Context, record *StoredCredential, consume func(func(string) error) error) (bool, error) {
	if record == nil || consume == nil {
		return false, errors.New("browser login executor: credential and consumer required")
	}
	var submitted bool
	err := consume(func(password string) error {
		expression := fmt.Sprintf(`(() => {
  const user = document.querySelector(%s);
  const pass = document.querySelector(%s);
  if (!user || !pass) return false;
  const set = (el, value) => { const setter = Object.getOwnPropertyDescriptor(el.__proto__, "value")?.set; if (setter) setter.call(el, value); else el.value = value; el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); };
  set(user, %s); set(pass, %s);
  const form = pass.form || user.form;
  if (form && typeof form.requestSubmit === "function") form.requestSubmit(); else if (form) form.submit(); else document.querySelector(%s)?.click();
  return true;
})()`, jsString(firstOr(e.selectors.UsernameField, `input[autocomplete="username"],input[type="email"],input[name*="user" i]`)), jsString(firstOr(e.selectors.PasswordField, `input[type="password"]`)), jsString(record.Username), jsString(password), jsString(firstOr(e.selectors.SubmitButton, `button[type="submit"],input[type="submit"]`)))
		if err := e.evaluate(ctx, expression, &submitted); err != nil {
			return err
		}
		if !submitted {
			return errors.New("login form fields unavailable")
		}
		return nil
	})
	return submitted && err == nil, err
}

func (e *BrowserLoginExecutor) MFAFieldVisible(ctx context.Context, _ *StoredCredential) (bool, error) {
	selector := firstOr(e.selectors.MFAField, `input[autocomplete="one-time-code"],input[name*="otp" i],input[name*="mfa" i],input[name*="code" i]`)
	var visible bool
	err := e.evaluate(ctx, fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return false; const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; })()`, jsString(selector)), &visible)
	return visible, err
}

func (e *BrowserLoginExecutor) SubmitMFA(ctx context.Context, code string) error {
	selector := firstOr(e.selectors.MFAField, `input[autocomplete="one-time-code"],input[name*="otp" i],input[name*="mfa" i],input[name*="code" i]`)
	var submitted bool
	if err := e.evaluate(ctx, fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return false; el.value = %s; el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); const form = el.form; if (form && typeof form.requestSubmit === "function") form.requestSubmit(); else if (form) form.submit(); return true; })()`, jsString(selector), jsString(code)), &submitted); err != nil {
		return err
	}
	if !submitted {
		return errors.New("mfa field unavailable")
	}
	return nil
}

func (e *BrowserLoginExecutor) VerifyAuthenticated(ctx context.Context, _ *StoredCredential) (AuthEvidence, error) {
	var evidence AuthEvidence
	for {
		var state struct {
			LoginForm bool     `json:"login_form"`
			Signals   []string `json:"signals"`
			URL       string   `json:"url"`
		}
		if err := e.evaluate(ctx, `(() => {
  const visible = (el) => { if (!el) return false; const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
  const signals = [];
  if (document.querySelector("a[href*='logout' i],button[data-testid*='logout' i], [aria-label*='account' i]")) signals.push("account_control");
  if (document.querySelector("[data-testid*='profile' i],[aria-label*='profile' i]")) signals.push("profile_control");
  return {login_form:visible(document.querySelector("input[type='password']")),signals:signals,url:location.href};
})()`, &state); err != nil {
			return evidence, err
		}
		evidence.LoginFormGone = !state.LoginForm
		evidence.AuthenticatedSignals = append([]string(nil), state.Signals...)
		if strings.Contains(strings.ToLower(state.URL), "login") || strings.Contains(strings.ToLower(state.URL), "signin") {
			evidence.ProtectedURL = false
		} else {
			evidence.ProtectedURL = true
		}
		cookieNames, err := e.cookieNames(ctx)
		if err == nil {
			evidence.CookieNames = cookieNames
		}
		if evidence.Verified() {
			return evidence, nil
		}
		select {
		case <-ctx.Done():
			return evidence, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (e *BrowserLoginExecutor) cookieNames(ctx context.Context) ([]string, error) {
	var result struct {
		Cookies []struct {
			Name string `json:"name"`
		} `json:"cookies"`
	}
	if err := e.page.Call(ctx, "Network.getAllCookies", nil, &result); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(result.Cookies))
	for _, cookie := range result.Cookies {
		if cookie.Name != "" {
			names = append(names, cookie.Name)
		}
	}
	return names, nil
}

func (e *BrowserLoginExecutor) evaluate(ctx context.Context, expression string, output any) error {
	var result struct {
		ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
		Result           struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := e.page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &result); err != nil {
		return err
	}
	if len(result.ExceptionDetails) > 0 && string(result.ExceptionDetails) != "null" {
		return errors.New("browser login executor: page evaluation failed")
	}
	if output == nil || len(result.Result.Value) == 0 || string(result.Result.Value) == "null" {
		return nil
	}
	return json.Unmarshal(result.Result.Value, output)
}

func jsString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func firstOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
