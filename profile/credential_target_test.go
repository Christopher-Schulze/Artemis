package profile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestCredentialTargetAllowedRequiresMatchingSecureOrigin(t *testing.T) {
	same := credentialTarget{Top: true, Protocol: "https:", Host: "bank.example", Hostname: "bank.example", FormFound: true, ActionTarget: []string{"https://bank.example"}}
	for _, tc := range []struct {
		name   string
		domain string
		mutate func(*credentialTarget)
		allow  bool
	}{
		{"same origin", "bank.example", func(*credentialTarget) {}, true},
		{"domain case and spacing", " Bank.Example ", func(*credentialTarget) {}, true},
		{"other page host", "bank.example", func(c *credentialTarget) { c.Hostname, c.Host = "evil.example", "evil.example" }, false},
		{"cross-origin form action", "bank.example", func(c *credentialTarget) { c.ActionTarget = []string{"https://evil.example"} }, false},
		{"cross-origin formaction button", "bank.example", func(c *credentialTarget) { c.ActionTarget = append(c.ActionTarget, "https://evil.example") }, false},
		{"https downgrade on submit", "bank.example", func(c *credentialTarget) { c.ActionTarget = []string{"http://bank.example"} }, false},
		{"plain http remote page", "bank.example", func(c *credentialTarget) { c.Protocol = "http:"; c.ActionTarget = []string{"http://bank.example"} }, false},
		{"embedded frame", "bank.example", func(c *credentialTarget) { c.Top = false }, false},
		{"no form", "bank.example", func(c *credentialTarget) { c.FormFound, c.ActionTarget = false, nil }, false},
		{"port-scoped domain", "bank.example:8443", func(c *credentialTarget) {
			c.Host = "bank.example:8443"
			c.ActionTarget = []string{"https://bank.example:8443"}
		}, true},
		{"loopback http", "127.0.0.1", func(c *credentialTarget) {
			c.Protocol, c.Hostname, c.Host = "http:", "127.0.0.1", "127.0.0.1:9000"
			c.ActionTarget = []string{"http://127.0.0.1:9000"}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := same
			target.ActionTarget = append([]string(nil), same.ActionTarget...)
			tc.mutate(&target)
			err := credentialTargetAllowed(tc.domain, target)
			if (err == nil) != tc.allow {
				t.Fatalf("allowed=%t, want %t (err=%v)", err == nil, tc.allow, err)
			}
		})
	}
}

func TestBrowserLoginNeverSendsSecretToCrossOriginFormTarget(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	var stolen atomic.Int64
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			stolen.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer attacker.Close()
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<!doctype html><title>Login</title><form method="post" action="%s/steal"><input name="username" autocomplete="username"><input name="password" type="password"><button type="submit">Sign in</button></form>`, attacker.URL)
	}))
	defer login.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second,
		AllowPrivateNetworks: true, AllowedPorts: []int{profileTestURLPort(t, login.URL), profileTestURLPort(t, attacker.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProfileTestResource(t, "browser close", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProfileTestResource(t, "browser context close", owner.Close)
	page, err := owner.NewPage(ctx, login.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProfileTestResource(t, "page close", page.Close)
	store, err := NewCredentialStore(t.TempDir()+"/credentials.enc", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreCredential("profile", "127.0.0.1", "user@example.com", "secret-password", LoginSelectors{}); err != nil {
		t.Fatal(err)
	}
	executor, err := NewBrowserLoginExecutor(page, LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireCredential("profile", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	submitted, err := executor.FillAndSubmit(ctx, lease.Record(), func(use func(string) error) error { return use("secret-password") })
	if submitted || !errors.Is(err, ErrCredentialTargetMismatch) {
		t.Fatalf("cross-origin form target was not rejected: submitted=%t err=%v", submitted, err)
	}
	time.Sleep(300 * time.Millisecond)
	if stolen.Load() != 0 {
		t.Fatalf("credential reached the cross-origin target %d time(s)", stolen.Load())
	}
}
