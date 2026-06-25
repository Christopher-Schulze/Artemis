package scraper

import (
	"net/http"
	"testing"
)

func TestCookieJar_SetAndGet(t *testing.T) {
	jar := NewCookieJar()
	cookies := []*http.Cookie{
		{Name: "session", Value: "abc123"},
		{Name: "token", Value: "xyz789"},
	}
	jar.SetCookies("example.com", cookies)
	if jar.CookieCount("example.com") != 2 {
		t.Fatalf("CookieCount = %d, want 2", jar.CookieCount("example.com"))
	}
	got := jar.Cookies("example.com")
	if len(got) != 2 {
		t.Fatalf("len(Cookies) = %d, want 2", len(got))
	}
	if got[0].Name != "session" || got[0].Value != "abc123" {
		t.Fatalf("cookie[0] = %+v, want session=abc123", got[0])
	}
}

func TestCookieJar_Empty(t *testing.T) {
	jar := NewCookieJar()
	if jar.CookieCount("nope.com") != 0 {
		t.Fatal("expected 0 cookies for unknown host")
	}
	if jar.ToHeader("nope.com") != "" {
		t.Fatal("expected empty header for unknown host")
	}
}

func TestCookieJar_ToHeader(t *testing.T) {
	jar := NewCookieJar()
	jar.SetCookies("host.com", []*http.Cookie{
		{Name: "a", Value: "1"},
		{Name: "b", Value: "2"},
	})
	h := jar.ToHeader("host.com")
	if h != "a=1; b=2" {
		t.Fatalf("ToHeader = %q, want 'a=1; b=2'", h)
	}
}

func TestCookieJar_Clear(t *testing.T) {
	jar := NewCookieJar()
	jar.SetCookies("h.com", []*http.Cookie{{Name: "x", Value: "y"}})
	jar.Clear()
	if jar.CookieCount("h.com") != 0 {
		t.Fatal("expected 0 cookies after clear")
	}
}

func TestCookieJar_AllCookies(t *testing.T) {
	jar := NewCookieJar()
	jar.SetCookies("a.com", []*http.Cookie{{Name: "s", Value: "1"}})
	jar.SetCookies("b.com", []*http.Cookie{{Name: "t", Value: "2"}})
	all := jar.AllCookies()
	if len(all) != 2 {
		t.Fatalf("AllCookies hosts = %d, want 2", len(all))
	}
}

func TestCookieJar_NilSafe(t *testing.T) {
	var jar *CookieJar
	jar.SetCookies("x", nil)
	if jar.Cookies("x") != nil {
		t.Fatal("nil jar should return nil cookies")
	}
	if jar.CookieCount("x") != 0 {
		t.Fatal("nil jar should return 0 count")
	}
	if jar.ToHeader("x") != "" {
		t.Fatal("nil jar should return empty header")
	}
	jar.Clear()
}

func TestDetectMetaCharset_CharsetAttr(t *testing.T) {
	html := []byte(`<html><head><meta charset="iso-8859-1"></head><body>hello</body></html>`)
	cs := detectMetaCharset(html)
	if cs != "iso-8859-1" {
		t.Fatalf("detectMetaCharset = %q, want iso-8859-1", cs)
	}
}

func TestDetectMetaCharset_HttpEquiv(t *testing.T) {
	html := []byte(`<html><head><meta http-equiv="Content-Type" content="text/html; charset=windows-1252"></head></html>`)
	cs := detectMetaCharset(html)
	if cs != "windows-1252" {
		t.Fatalf("detectMetaCharset = %q, want windows-1252", cs)
	}
}

func TestDetectMetaCharset_None(t *testing.T) {
	html := []byte(`<html><body>no meta charset</body></html>`)
	cs := detectMetaCharset(html)
	if cs != "" {
		t.Fatalf("detectMetaCharset = %q, want empty", cs)
	}
}

func TestDetectMetaCharset_EmptyBody(t *testing.T) {
	cs := detectMetaCharset(nil)
	if cs != "" {
		t.Fatalf("detectMetaCharset(nil) = %q, want empty", cs)
	}
}

func TestDetectBOM_UTF8(t *testing.T) {
	body := []byte{0xEF, 0xBB, 0xBF, 'h', 'i'}
	if cs := detectBOM(body); cs != "utf-8" {
		t.Fatalf("detectBOM = %q, want utf-8", cs)
	}
}

func TestDetectBOM_UTF16LE(t *testing.T) {
	body := []byte{0xFF, 0xFE, 0x00, 0x00}
	if cs := detectBOM(body); cs != "utf-16le" {
		t.Fatalf("detectBOM = %q, want utf-16le", cs)
	}
}

func TestDetectBOM_UTF16BE(t *testing.T) {
	body := []byte{0xFE, 0xFF, 0x00, 0x00}
	if cs := detectBOM(body); cs != "utf-16be" {
		t.Fatalf("detectBOM = %q, want utf-16be", cs)
	}
}

func TestDetectBOM_None(t *testing.T) {
	body := []byte("plain text")
	if cs := detectBOM(body); cs != "" {
		t.Fatalf("detectBOM = %q, want empty", cs)
	}
}

func TestDecodeBody_UTF8(t *testing.T) {
	body := []byte("hello world")
	text, err := decodeBody(body, "utf-8")
	if err != nil {
		t.Fatalf("decodeBody error: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q, want 'hello world'", text)
	}
}

func TestDecodeBody_UnknownEncoding(t *testing.T) {
	_, err := decodeBody([]byte("test"), "nonexistent-encoding")
	if err == nil {
		t.Fatal("expected error for unknown encoding")
	}
}

func TestDecodeBody_FallbackHandled(t *testing.T) {
	// Even with a bad encoding name, the caller should fallback to utf-8.
	// This tests that decodeBody returns an error (not panic).
	_, err := decodeBody([]byte("test"), "bad-enc")
	if err == nil {
		t.Fatal("expected error for bad encoding")
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/path", "example.com"},
		{"http://sub.example.com:8080/page", "sub.example.com"},
		{"https://Example.COM/Path", "example.com"},
		{"example.com/path", "example.com"},
		{"https://site.org", "site.org"},
	}
	for _, tt := range tests {
		got := extractHost(tt.url)
		if got != tt.want {
			t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestParseSetCookie(t *testing.T) {
	headers := http.Header{}
	headers.Add("Set-Cookie", "session=abc123; Path=/; HttpOnly")
	headers.Add("Set-Cookie", "token=xyz789; Secure")

	cookies := parseSetCookie(headers)
	if len(cookies) != 2 {
		t.Fatalf("len(cookies) = %d, want 2", len(cookies))
	}
	if cookies[0].Name != "session" || cookies[0].Value != "abc123" {
		t.Fatalf("cookie[0] = %+v, want session=abc123", cookies[0])
	}
	if cookies[1].Name != "token" || cookies[1].Value != "xyz789" {
		t.Fatalf("cookie[1] = %+v, want token=xyz789", cookies[1])
	}
}

func TestParseSetCookie_Empty(t *testing.T) {
	cookies := parseSetCookie(http.Header{})
	if len(cookies) != 0 {
		t.Fatalf("len(cookies) = %d, want 0", len(cookies))
	}
}

func TestParseSetCookie_Nil(t *testing.T) {
	cookies := parseSetCookie(nil)
	if cookies != nil {
		t.Fatalf("expected nil for nil headers")
	}
}

func TestParseSetCookie_Malformed(t *testing.T) {
	headers := http.Header{}
	headers.Add("Set-Cookie", "noequalsign")
	headers.Add("Set-Cookie", "good=value")
	cookies := parseSetCookie(headers)
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1 (malformed skipped)", len(cookies))
	}
	if cookies[0].Name != "good" {
		t.Fatalf("cookie[0].Name = %q, want good", cookies[0].Name)
	}
}

func TestNewStaticDetailFetcher_Defaults(t *testing.T) {
	f := NewStaticDetailFetcher(nil, nil, 0, 2)
	if f == nil {
		t.Fatal("NewStaticDetailFetcher returned nil")
	}
	if f.jar == nil {
		t.Fatal("jar should be auto-created when nil")
	}
	if f.maxRetries != 2 {
		t.Fatalf("maxRetries = %d, want 2", f.maxRetries)
	}
}

func TestStaticDetailResult_Fields(t *testing.T) {
	r := &StaticDetailResult{
		StatusCode:  200,
		ContentType: "text/html; charset=utf-8",
		Charset:     "utf-8",
		Encoding:    "utf-8",
		Text:        "hello",
		Body:        []byte("hello"),
		FinalURL:    "https://example.com/page",
	}
	if r.StatusCode != 200 {
		t.Fatalf("StatusCode = %d", r.StatusCode)
	}
	if r.Encoding != "utf-8" {
		t.Fatalf("Encoding = %q", r.Encoding)
	}
	if r.Text != "hello" {
		t.Fatalf("Text = %q", r.Text)
	}
}
