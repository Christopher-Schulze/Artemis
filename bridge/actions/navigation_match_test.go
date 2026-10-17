package actions

import "testing"

func TestNavigationPostconditionMatchesExactDestination(t *testing.T) {
	tests := []struct {
		name, requested, actual string
		want                    bool
	}{
		{"root slash", "https://example.com", "https://example.com/", true},
		{"default port", "https://EXAMPLE.com:443/a", "https://example.com/a", true},
		{"dot segments", "https://example.com/a/../b", "https://example.com/b", true},
		{"blank", "about:blank", "about:blank", true},
		{"host prefix attack", "https://example.com", "https://example.com.evil/", false},
		{"path prefix", "https://example.com/order", "https://example.com/orders", false},
		{"scheme redirect", "http://example.com/", "https://example.com/", false},
		{"port redirect", "https://example.com:8443/", "https://example.com/", false},
		{"query difference", "https://example.com/?id=1", "https://example.com/?id=12", false},
		{"fragment difference", "https://example.com/#a", "https://example.com/#b", false},
		{"encoded slash", "https://example.com/a%2Fb", "https://example.com/a/b", false},
		{"double slash", "https://example.com/a//b", "https://example.com/a/b", false},
		{"userinfo", "https://example.com/", "https://user@example.com/", false},
		{"relative", "/order", "https://example.com/order", false},
		{"missing HTTP host", "https:///order", "https:///order", false},
		{"other schemes stay exact", "file:///a/../b", "file:///b", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameNavigationURL(test.requested, test.actual); got != test.want {
				t.Fatalf("requested=%q actual=%q match=%v want=%v", test.requested, test.actual, got, test.want)
			}
		})
	}
}
