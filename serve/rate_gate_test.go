package serve

import (
	"strings"
	"testing"
)

// A refused message is answered under its request ID without decoding the
// rest: the ID is read token by token and the payload behind it is never
// validated (review point 73).
func TestLeadingRequestIDStopsAtTheID(t *testing.T) {
	for _, test := range []struct {
		name, message, want string
	}{
		{name: "id first, payload never read", message: `{"id":"req-1","cmd":"navigate","params":` + strings.Repeat("[", 1000) + `INVALID`, want: "req-1"},
		{name: "id after small fields", message: `{"version":"1","id":"req-2","params":{"url":"https://example.com"}}`, want: "req-2"},
		{name: "no id", message: `{"cmd":"heartbeat"}`, want: ""},
		{name: "not an object", message: `["id","req-3"]`, want: ""},
		{name: "numeric id", message: `{"id":7}`, want: ""},
	} {
		if got := leadingRequestID([]byte(test.message)); got != test.want {
			t.Fatalf("%s: id %q, want %q", test.name, got, test.want)
		}
	}
}
