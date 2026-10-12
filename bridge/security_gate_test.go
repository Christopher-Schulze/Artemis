package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestSecurityGateChromiumSSRFMutationMatrix(t *testing.T) {
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	owner, err := browser.NewContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(context.Background(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"http://2130706433/",
		"http://017700000001/",
		"http://0x7f000001/",
		"http://[::ffff:127.0.0.1]/",
		"http://metadata.google.internal/latest/meta-data/",
		"http://metadata.goog/computeMetadata/v1/",
	} {
		if _, _, err := page.Navigate(context.Background(), target); !errors.Is(err, network.ErrPolicyDenied) {
			t.Fatalf("navigation %q error=%v, want policy denial", target, err)
		}
	}
	if !browser.Healthy() {
		t.Fatalf("SSRF denials damaged browser health: %v", browser.Err())
	}
}

func TestSecurityGateChromiumRedirectChainRevalidatesEveryHop(t *testing.T) {
	var deniedHits atomic.Int64
	denied := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		deniedHits.Add(1)
	}))
	defer denied.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/start":
			http.Redirect(writer, request, "/hop", http.StatusFound)
		case "/hop":
			http.Redirect(writer, request, denied.URL+"/secret", http.StatusTemporaryRedirect)
		default:
			_, _ = fmt.Fprint(writer, "allowed")
		}
	}))
	defer allowed.Close()
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{
		AllowPrivateNetworks: true,
		AllowedPorts:         []int{testURLPort(t, allowed.URL)},
	})
	owner, err := browser.NewContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(context.Background(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := page.Navigate(context.Background(), allowed.URL+"/start"); err == nil {
		t.Fatal("multi-hop redirect reached a disallowed destination")
	}
	if got := deniedHits.Load(); got != 0 {
		t.Fatalf("disallowed redirect destination received %d requests", got)
	}
	if !browser.Healthy() {
		t.Fatalf("redirect denial damaged browser health: %v", browser.Err())
	}
}

func TestSecurityGateChromiumUnknownPostBodyFailsClosed(t *testing.T) {
	contentLength := fetchContentLength(nil, "", true)
	if contentLength != -1 {
		t.Fatalf("unknown Chromium post body length=%d", contentLength)
	}
	policy, err := network.NewPolicy(network.PolicyConfig{MaxRequestBodyBytes: 8}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateRequest(context.Background(), "https://public.example/upload", http.MethodPost, "application/octet-stream", contentLength, network.TargetSubresource, "security-gate"); !errors.Is(err, network.ErrPolicyDenied) {
		t.Fatalf("unknown Chromium post body error=%v", err)
	}
}
