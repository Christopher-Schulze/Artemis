package cookiejar

import (
	"net/http"
	standard "net/http/cookiejar"
	"net/url"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/publicsuffix"
)

func TestHTTPSelectionMatchesGoCookieJar(t *testing.T) {
	actual, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := standard.New(&standard.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ target, line string }{
		{"https://www.example.test/a/seed", "host=one"},
		{"https://www.example.test/a/seed", "domain=two; Domain=.example.test; Path=/"},
		{"https://www.example.test/a/seed", "secure=three; Secure; HttpOnly; Path=/"},
		{"https://www.example.test/a/seed", "host=changed"},
		{"https://www.example.test/a/seed", "deleted=x; Path=/; Max-Age=0"},
		{"https://www.example.test/a/seed", "rejected=x; Domain=test; Path=/"},
		{"https://www.example.test/a/seed", "foreign=x; Domain=other.test; Path=/"},
		{"https://bücher.test/a", "idn=actual; Path=/"},
		{"http://127.0.0.1/a", "loopback=actual; Secure; Path=/"},
		{"https://[::1]/a", "ipv6=actual; Path=/"},
	} {
		response := http.Response{Header: http.Header{"Set-Cookie": {step.line}}}
		target := testCookieURL(t, step.target)
		actual.SetCookies(target, response.Cookies())
		expected.SetCookies(target, response.Cookies())
		for _, query := range []string{"https://www.example.test/a/path", "https://www.example.test/", "https://other.example.test/", "http://www.example.test/a/path", "https://bücher.test/", "https://xn--bcher-kva.test/", "http://127.0.0.1/", "https://[::1]/", "ftp://www.example.test/a"} {
			u := testCookieURL(t, query)
			if got, want := actual.Cookies(u), expected.Cookies(u); !reflect.DeepEqual(got, want) {
				t.Fatalf("HTTP selection drift after %q at %s: got=%+v want=%+v", step.line, query, got, want)
			}
		}
	}
}

func TestDocumentAuthorityKeepsSameNamePathsAndHTTPStateSeparate(t *testing.T) {
	jar, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u := testCookieURL(t, "https://www.example.test/sub/page")
	jar.SetCookies(u, []*http.Cookie{{Name: "auth", Value: "secret", Path: "/", HttpOnly: true}, {Name: "auth", Value: "public", Path: "/sub"}})
	if got := jar.DocumentCookies(u); len(got) != 1 || got[0].Value != "public" {
		t.Fatalf("HttpOnly leaked or same-name visible cookie disappeared: %+v", got)
	}
	for _, cookie := range []*http.Cookie{
		{Name: "auth", Value: "forged", Path: "/"}, {Name: "auth", MaxAge: -1, Path: "/"},
		{Name: "created", Value: "forged", HttpOnly: true, Path: "/"},
	} {
		jar.SetDocumentCookies(u, []*http.Cookie{cookie})
		if got := jar.Cookies(u); len(got) != 2 || got[1].Value != "secret" {
			t.Fatalf("document changed HTTP-only state: cookie=%+v got=%+v", cookie, got)
		}
	}
	jar.SetDocumentCookies(u, []*http.Cookie{{Name: "auth", Value: "script", Path: "/sub"}})
	if got := jar.DocumentCookies(u); len(got) != 1 || got[0].Value != "script" {
		t.Fatalf("public same-name write was blocked: %+v", got)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "auth", Value: "server-update", Path: "/", HttpOnly: true}})
	if got := jar.Cookies(u); len(got) != 2 || got[1].Value != "server-update" {
		t.Fatalf("real HTTP owner could not update its secret: %+v", got)
	}
}

func TestDocumentAuthorityProtectsParentDomainAndSecureCookies(t *testing.T) {
	jar, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u := testCookieURL(t, "https://auth.example.test/account/page")
	jar.SetCookies(u, []*http.Cookie{{Name: "auth", Value: "secret", Domain: ".example.test", Path: "/account", HttpOnly: true, Secure: true}})
	child := testCookieURL(t, "http://child.example.test/account/page")
	for _, cookie := range []*http.Cookie{
		{Name: "auth", Value: "forged", Domain: ".example.test", Path: "/account"},
		{Name: "auth", Value: "forged", Domain: ".example.test", Path: "/account/sub"},
		{Name: "created", Value: "forged", Secure: true, Path: "/"},
	} {
		jar.SetDocumentCookies(child, []*http.Cookie{cookie})
	}
	if got := jar.Cookies(u); len(got) != 1 || got[0].Value != "secret" {
		t.Fatalf("insecure/child document crossed cookie authority: %+v", got)
	}
	if got := jar.DocumentCookies(child); len(got) != 0 {
		t.Fatalf("insecure document sees private or Secure cookies: %+v", got)
	}
}

func TestExpiredHTTPOnlyDoesNotBlockLegitimateDocumentReplacement(t *testing.T) {
	jar, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u := testCookieURL(t, "https://example.test/")
	jar.setCookies(u, []*http.Cookie{{Name: "auth", Value: "expired", Path: "/", MaxAge: 1, HttpOnly: true}}, time.Now().Add(-2*time.Second), false)
	jar.SetDocumentCookies(u, []*http.Cookie{{Name: "auth", Value: "current", Path: "/"}})
	if got := jar.DocumentCookies(u); len(got) != 1 || got[0].Value != "current" {
		t.Fatalf("expired flag retained authority over new state: %+v", got)
	}
}

func TestPunycodeDigitsAndDomainVectors(t *testing.T) {
	for _, test := range []struct {
		digit int32
		want  byte
	}{{0, 'a'}, {25, 'z'}, {26, '0'}, {35, '9'}} {
		if got, err := encodeDigit(test.digit); err != nil || got != test.want {
			t.Fatalf("punycode digit %d: %q %v", test.digit, got, err)
		}
	}
	for _, digit := range []int32{-1, 36} {
		if _, err := encodeDigit(digit); err == nil {
			t.Fatalf("invalid digit %d accepted", digit)
		}
	}
	if got, err := toASCII("bücher.example.test"); err != nil || got != "xn--bcher-kva.example.test" {
		t.Fatalf("punycode domain: %q %v", got, err)
	}
}

func testCookieURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
