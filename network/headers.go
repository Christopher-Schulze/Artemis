package network

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// ValidatedHeaders returns a detached, canonical bounded header snapshot.
// Conflicting case spellings and ambiguous content identity fail closed.
func ValidatedHeaders(headers http.Header) (http.Header, error) {
	if len(headers) > 128 {
		return nil, fmt.Errorf("%w: too_many_headers", ErrPolicyDenied)
	}
	result := make(http.Header, len(headers))
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	total := 0
	for _, key := range keys {
		canonical := http.CanonicalHeaderKey(key)
		if len(key) > 256 || !httpguts.ValidHeaderFieldName(key) {
			return nil, fmt.Errorf("%w: invalid_header_name", ErrPolicyDenied)
		}
		if _, exists := result[canonical]; exists {
			return nil, fmt.Errorf("%w: ambiguous_header_name", ErrPolicyDenied)
		}
		values := headers[key]
		var err error
		total, err = validateHeaderValues(canonical, values, total+len(key))
		if err != nil {
			return nil, err
		}
		result[canonical] = append([]string(nil), values...)
	}
	return result, nil
}

func validateHeaderValues(key string, values []string, total int) (int, error) {
	if len(values) > 128 {
		return 0, fmt.Errorf("%w: too_many_header_values", ErrPolicyDenied)
	}
	if (key == "Content-Type" || key == "Content-Length" || key == "Content-Encoding" || key == "Authorization") && len(values) != 1 {
		return 0, fmt.Errorf("%w: ambiguous_header_value", ErrPolicyDenied)
	}
	for _, value := range values {
		total += len(value)
		if len(value) > 4096 || total > 32<<10 || !httpguts.ValidHeaderFieldValue(value) {
			return 0, fmt.Errorf("%w: invalid_header_value", ErrPolicyDenied)
		}
	}
	return total, nil
}

func (c *HTTPClient) applyRequestHeaders(request *http.Request, headers http.Header) error {
	validated, err := ValidatedHeaders(headers)
	if err != nil {
		return err
	}
	for _, key := range []string{"Host", "Transfer-Encoding", "Proxy-Authorization", "Proxy-Connection"} {
		if _, supplied := validated[key]; supplied {
			return fmt.Errorf("%w: transport_owned_header", ErrPolicyDenied)
		}
	}
	if raw, supplied := validated["Content-Length"]; supplied {
		length, err := strconv.ParseInt(raw[0], 10, 64)
		if err != nil || length < 0 || length != policyRequestContentLength(request) {
			return fmt.Errorf("%w: request_length_mismatch", ErrPolicyDenied)
		}
		delete(validated, "Content-Length")
	}
	request.Header = validated
	if _, supplied := validated["User-Agent"]; !supplied && c.cfg.UserAgent != "" {
		request.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	for key, values := range c.chromeHeaders {
		if _, supplied := validated[key]; !supplied {
			request.Header[key] = append([]string(nil), values...)
		}
	}
	if len(request.Header.Values("Content-Type")) == 1 {
		request.Header.Set("Content-Type", strings.TrimSpace(request.Header.Get("Content-Type")))
	}
	return nil
}
