package serve

import (
	"errors"
	"io"
	"net"
	"net/url"
)

// observedNetworkFailure requires a real transport/DNS/HTTP-timeout cause.
// url.Error alone also wraps application/policy errors and is insufficient.
func observedNetworkFailure(err error) bool {
	var socket *net.OpError
	var dns *net.DNSError
	if errors.As(err, &socket) || errors.As(err, &dns) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var request *url.Error
	if !errors.As(err, &request) {
		return false
	}
	var timeout net.Error
	return errors.As(request.Err, &timeout) && timeout.Timeout()
}
