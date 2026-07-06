package scraper

import "testing"

func TestClassifyContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want ContentTypeCategory
	}{
		{"text/html; charset=utf-8", CategoryHTML},
		{"application/json", CategoryJSON},
		{"application/xml", CategoryXML},
		{"text/plain", CategoryText},
		{"image/png", CategoryBinary},
	}
	for _, tc := range cases {
		got := ClassifyContentType(tc.ct)
		if got != tc.want {
			t.Fatalf("ClassifyContentType(%q) = %q, want %q", tc.ct, got, tc.want)
		}
	}
}

func TestExtractCharset(t *testing.T) {
	if got := extractCharset("text/html; charset=utf-8"); got != "utf-8" {
		t.Fatalf("expected utf-8, got %q", got)
	}
	if got := extractCharset("text/html"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestShouldRetry(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		if !ShouldRetry(code) {
			t.Fatalf("expected retry for %d", code)
		}
	}
	if ShouldRetry(200) {
		t.Fatal("expected no retry for 200")
	}
}
