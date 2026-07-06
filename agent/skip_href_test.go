package agent

import "testing"

func TestHasPrefixFold(t *testing.T) {
	tests := []struct {
		s      string
		prefix string
		want   bool
	}{
		{"javascript:alert(1)", "javascript:", true},
		{"JavaScript:alert(1)", "javascript:", true},
		{"JAVASCRIPT:alert(1)", "javascript:", true},
		{"mailto:test@example.com", "mailto:", true},
		{"MailTo:test@example.com", "mailto:", true},
		{"tel:+1234567890", "tel:", true},
		{"TEL:+1234567890", "tel:", true},
		{"data:text/html,<h1>hi</h1>", "data:", true},
		{"DATA:text/html,<h1>hi</h1>", "data:", true},
		{"https://example.com", "javascript:", false},
		{"http://example.com", "mailto:", false},
		{"", "javascript:", false},
		{"java", "javascript:", false},
		{"javascript", "javascript:", false},
	}
	for _, tt := range tests {
		t.Run(tt.s+"/"+tt.prefix, func(t *testing.T) {
			if got := hasPrefixFold(tt.s, tt.prefix); got != tt.want {
				t.Errorf("hasPrefixFold(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestSkipHref(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"", true},
		{"#fragment", true},
		{"javascript:alert(1)", true},
		{"JavaScript:alert(1)", true},
		{"mailto:test@example.com", true},
		{"MailTo:test@example.com", true},
		{"tel:+1234567890", true},
		{"TEL:+1234567890", true},
		{"data:text/html,<h1>hi</h1>", true},
		{"DATA:text/html,<h1>hi</h1>", true},
		{"https://example.com", false},
		{"http://example.com", false},
		{"/relative/path", false},
		{"./relative", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := skipHref(tt.raw); got != tt.want {
				t.Errorf("skipHref(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func BenchmarkHasPrefixFold(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasPrefixFold("JavaScript:alert(document.cookie)", "javascript:")
	}
}

func BenchmarkSkipHref(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = skipHref("https://example.com/page")
	}
}
