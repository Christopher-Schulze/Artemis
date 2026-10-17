package serve

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"syscall"
	"testing"
)

func TestObservedNetworkFailureRetainsCauseWithoutMessageGuessing(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		want  bool
	}{
		{"socket refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"DNS unavailable", &net.DNSError{Err: "no such host", Name: "example.invalid"}, true},
		{"HTTP timeout", &url.Error{Op: "Get", URL: "https://example.test", Err: context.DeadlineExceeded}, true},
		{"truncated HTTP body", io.ErrUnexpectedEOF, true},
		{"HTTP application policy", &url.Error{Op: "Get", URL: "https://example.test", Err: errors.New("policy denied")}, false},
		{"network looking message", errors.New("fetch_failed: DNS timeout EOF"), false},
		{"caller canceled", context.Canceled, false},
		{"body budget", errors.New("body byte limit exceeded"), false},
	}
	server := &Server{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := observedNetworkFailure(tc.cause); actual != tc.want {
				t.Fatalf("actual cause classified=%v want=%v", actual, tc.want)
			}
			response := server.taskErrorResponse(&Request{ID: "observed"}, tc.cause, ErrFetchFailed)
			if (response.Error.FailureKind == FailureNetwork) != tc.want || response.Error.Code != string(ErrFetchFailed) {
				t.Fatalf("wire cause differs: %+v", response.Error)
			}
			response = server.taskErrorResponse(&Request{ID: "other"}, tc.cause, ErrBadParams)
			if response.Error.FailureKind != "" {
				t.Fatal("non-fetch failure became external-network tolerance")
			}
		})
	}
}
