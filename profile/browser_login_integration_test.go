package profile

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestBrowserLoginExecutorRealChromiumCredentialAndPostcondition(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.FormValue("username") == "user@example.com" && r.FormValue("password") == "secret-password" {
			http.SetCookie(w, &http.Cookie{Name: "auth_session", Value: "opaque", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<!doctype html><title>Account</title><a href="/logout">Log out</a><main>authenticated</main>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!doctype html><title>Login</title><form method="post"><input name="username" autocomplete="username"><input name="password" type="password"><button type="submit">Sign in</button></form>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second,
		AllowPrivateNetworks: true, AllowedPorts: []int{profileTestURLPort(t, server.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	page, err := owner.NewPage(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	store, err := NewCredentialStore(t.TempDir()+"/credentials.enc", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	if _, storeErr := store.StoreCredential("profile", "127.0.0.1", "user@example.com", "secret-password", LoginSelectors{}); storeErr != nil {
		t.Fatal(storeErr)
	}
	executor, err := NewBrowserLoginExecutor(page, LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	auth := &Authenticator{Store: store, Executor: executor, Policy: AuthenticationPolicy{AllowedDomains: []string{"127.0.0.1"}, AllowedModes: []AuthenticationMode{AuthModeProvidedCredentials}, MaxDuration: 5 * time.Second}}
	outcome, err := auth.Authenticate(ctx, AuthenticationRequest{ProfileName: "profile", Domain: "127.0.0.1", Purpose: "fixture", Mode: AuthModeProvidedCredentials})
	if err != nil || outcome.Status != AuthStatusAuthenticated || !outcome.Evidence.Verified() {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	for _, name := range outcome.Evidence.CookieNames {
		if name == "secret-password" {
			t.Fatal("password leaked as cookie evidence")
		}
	}
}
