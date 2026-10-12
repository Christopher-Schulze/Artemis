package network

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func encodeResponseFixture(t *testing.T, body []byte, encoding string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	var writer io.WriteCloser
	var err error
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&buffer)
	case "deflate":
		writer = zlib.NewWriter(&buffer)
	case "raw deflate":
		writer, err = flate.NewWriter(&buffer, flate.DefaultCompression)
	case "br":
		writer = brotli.NewWriter(&buffer)
	case "zstd":
		writer, err = zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
	default:
		t.Fatalf("unknown fixture encoding %s", encoding)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestHTTPClientDecodesAdvertisedResponseEncodings(t *testing.T) {
	for _, encoding := range []string{"gzip", "deflate", "raw deflate", "br", "zstd", "gzip, br"} {
		t.Run(encoding, func(t *testing.T) {
			plain := []byte("<html><title>Artemis compressed fixture</title><body>actual page</body></html>")
			wireEncoding := encoding
			if encoding == "raw deflate" {
				wireEncoding = "deflate"
			}
			var body []byte
			if encoding == "gzip, br" {
				body = encodeResponseFixture(t, encodeResponseFixture(t, plain, "gzip"), "br")
			} else {
				body = encodeResponseFixture(t, plain, encoding)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept-Encoding") != "gzip, deflate, br, zstd" {
					t.Error("fixture did not exercise actual Chrome encoding negotiation")
				}
				w.Header().Set("Content-Encoding", wireEncoding)
				w.Header().Set("Content-Type", "text/html")
				if _, err := w.Write(body); err != nil {
					t.Errorf("write compressed fixture: %v", err)
				}
			}))
			defer server.Close()
			lifecycle := &testRequestLifecycle{}
			client := newTestClient(t, HTTPClientConfig{ChromeLike: true, MaxBodyBytes: 1 << 20, RequestLifecycle: lifecycle})
			response, err := client.Do(t.Context(), Request{URL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(response.Body, plain) || response.Headers.Get("Content-Encoding") != "" || response.Headers.Get("Content-Length") != "" {
				t.Fatalf("decoded response or metadata incorrect: %q %v", response.Body, response.Headers)
			}
			wantBytes := len(body)
			if len(plain) > wantBytes {
				wantBytes = len(plain)
			}
			if lifecycle.started != 1 || lifecycle.finished != 1 || lifecycle.bytes != int64(wantBytes) {
				t.Fatalf("decoded response escaped lifecycle accounting: %+v", lifecycle)
			}
		})
	}
}

func TestResponseDecodingRejectsExpansionCorruptionAndUnknownEncoding(t *testing.T) {
	for _, encoding := range []string{"gzip", "deflate", "br", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			body := encodeResponseFixture(t, []byte(strings.Repeat("x", 8192)), encoding)
			if _, err := decodeResponseBody(body, encoding, 4096); err == nil {
				t.Fatal("decoder accepted expansion beyond output limit")
			}
			if _, err := decodeResponseBody([]byte{0xff}, encoding, 4096); err == nil {
				t.Fatal("decoder accepted corrupt response")
			}
		})
	}
	for _, encoding := range []string{"unknown", "identity, identity, identity, identity, identity"} {
		if _, err := decodeResponseBody([]byte("body"), encoding, 4096); err == nil {
			t.Fatalf("decoder accepted %s", encoding)
		}
	}
	if _, err := decodeResponseBody([]byte("long body"), "identity", 1); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("encoded input limit not enforced: %v", err)
	}
}
