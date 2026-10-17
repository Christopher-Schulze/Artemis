package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestResponseScopeGoInputsHoldActualCapacity(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		size    int64
	}{
		{"small", []byte("actual small response"), 21},
		{"unmapped_large", bytes.Repeat([]byte("x"), (1<<20)+1), (1 << 20) + 1},
		{"unknown", bytes.Repeat([]byte("y"), 65537), -1},
		{"empty", nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &scopeMemoryOwner{}
			client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
			client.mmap = platform.ProbeResult{State: platform.StateUnknown, Reason: "unobserved mapping support"}
			body, release, proof, err := client.readScopedBody(t.Context(), bytes.NewReader(test.payload), test.size, 2<<20, true)
			if err != nil || !bytes.Equal(body, test.payload) || owner.held != int64(cap(body)) {
				t.Fatalf("actual Go storage was not held: %v bytes=%d capacity=%d held=%d", err, len(body), cap(body), owner.held)
			}
			if test.size > 1<<20 && proof.State != platform.StateUnknown {
				t.Fatalf("Go admission fabricated mmap proof: %+v", proof)
			}
			if err := releaseResponseInputs(release)(); err != nil || owner.held != 0 {
				t.Fatalf("Go cleanup failed: %v held=%d", err, owner.held)
			}
		})
	}
}

type capacityResponseOwner struct {
	scopeMemoryOwner
	limit int64
}

var errResponseCapacity = errors.New("actual response capacity denied")

func (o *capacityResponseOwner) ReserveMemory(ctx context.Context, size int64) (func() error, error) {
	if size > o.limit-o.held {
		return nil, errResponseCapacity
	}
	return o.scopeMemoryOwner.ReserveMemory(ctx, size)
}

func TestResponseGoGrowthDenialRetainsOldAllocation(t *testing.T) {
	owner := &capacityResponseOwner{limit: 65536}
	reader := bytes.NewReader(bytes.Repeat([]byte("x"), 32769))
	body, release, err := readAdmittedResponse(t.Context(), reader, -1, 1<<20, owner)
	if !errors.Is(err, errResponseCapacity) || body != nil || release == nil || owner.held != 32768 {
		t.Fatalf("replacement bypassed overlap admission: %v held=%d", err, owner.held)
	}
	if err := release(); err != nil || owner.held != 0 || owner.releases != 1 {
		t.Fatalf("old allocation lost its release: %v owner=%+v", err, owner)
	}
}

func TestResponseGoReadFailuresRetainCleanup(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		size    int64
		limit   int64
		want    error
	}{
		{"truncated", []byte("short"), 100, 100, io.ErrUnexpectedEOF},
		{"declared_overrun", []byte("long"), 3, 100, nil},
		{"unknown_overrun", []byte("long"), -1, 3, ErrBodyTooLarge},
		{"invalid_limit", nil, -1, 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &scopeMemoryOwner{}
			_, release, err := readAdmittedResponse(t.Context(), bytes.NewReader(test.payload), test.size, test.limit, owner)
			if err == nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("invalid body accepted: %v", err)
			}
			if err := releaseResponseInputs(release)(); err != nil || owner.held != 0 {
				t.Fatalf("failed read lost cleanup: %v held=%d", err, owner.held)
			}
		})
	}
}

func TestResponseScopeGoCancellationAndSettlement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "actual small body"); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	settlement := errors.New("settlement failure")
	owner := &scopeMemoryOwner{releaseErr: settlement}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
	ctx, cancel := context.WithCancel(t.Context())
	var borrowed *Response
	err := client.WithResponse(ctx, Request{URL: server.URL}, TargetNavigation, func(response *Response) error {
		borrowed = response
		cancel()
		if owner.held != int64(cap(response.Body)) || owner.held == 0 {
			return errors.New("cancellation freed live bytes")
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, settlement) || borrowed == nil || borrowed.Body != nil || owner.held != 0 || owner.releases != 1 {
		t.Fatalf("cancellation/settlement boundary failed: %v owner=%+v", err, owner)
	}
}

func TestResponseScopeAdmitsEachActualDecodedLayer(t *testing.T) {
	for _, encoding := range []string{"gzip", "deflate", "raw deflate", "br", "zstd", "gzip, br"} {
		t.Run(encoding, func(t *testing.T) {
			plain := []byte(strings.Repeat("actual compressed response ", 4096))
			wireEncoding := encoding
			if encoding == "raw deflate" {
				wireEncoding = "deflate"
			}
			var encoded []byte
			if encoding == "gzip, br" {
				encoded = encodeResponseFixture(t, encodeResponseFixture(t, plain, "gzip"), "br")
			} else {
				encoded = encodeResponseFixture(t, plain, encoding)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", wireEncoding)
				w.Header().Set("Content-Length", fmt.Sprint(len(encoded)))
				if _, err := w.Write(encoded); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			owner := &scopeMemoryOwner{}
			client := newTestClient(t, HTTPClientConfig{ChromeLike: true, MaxBodyBytes: 1 << 20, MemoryAdmission: owner})
			err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error {
				if !bytes.Equal(response.Body, plain) || owner.held < int64(len(encoded)+cap(response.Body)) || response.Headers.Get("Content-Encoding") != "" {
					return fmt.Errorf("decoded output escaped ownership: held=%d output=%d", owner.held, cap(response.Body))
				}
				return nil
			})
			if err != nil || owner.held != 0 || owner.releases < 3 {
				t.Fatalf("codec/output ownership failed: %v owner=%+v", err, owner)
			}
		})
	}
}

func TestResponseScopeDecoderDeniedBeforeConsumer(t *testing.T) {
	encoded := encodeResponseFixture(t, []byte("actual gzip output"), "gzip")
	owner := &capacityResponseOwner{limit: 1 << 20}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
	_, release, err := client.decodeScopedResponse(t.Context(), encoded, "gzip", 4096)
	if !errors.Is(err, errResponseCapacity) {
		// A workspace of exactly one MiB fits; output admission must then fail.
		t.Fatalf("decoder output bypassed held workspace: %v", err)
	}
	if err := releaseResponseInputs(release)(); err != nil || owner.held != 0 {
		t.Fatalf("decoder denial leaked its workspace/output: %v held=%d", err, owner.held)
	}
}

func TestResponseScopeHEADDoesNotAllocateAdvertisedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12345")
		w.Header().Set("Content-Encoding", "gzip")
	}))
	defer server.Close()
	owner := &scopeMemoryOwner{}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
	err := client.WithResponse(t.Context(), Request{URL: server.URL, Method: http.MethodHead}, TargetNavigation, func(response *Response) error {
		if len(response.Body) != 0 || owner.held != 0 {
			return errors.New("HEAD allocated an absent advertised body")
		}
		return nil
	})
	if err != nil || owner.held != 0 || owner.releases != 0 {
		t.Fatalf("HEAD body handling failed: %v owner=%+v", err, owner)
	}
}

func TestResponseScopeChunkedPeerRetainsGoCapacity(t *testing.T) {
	payload := strings.Repeat("actual chunk ", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		if _, err := io.WriteString(w, payload); err != nil {
			t.Errorf("write chunked response: %v", err)
		}
	}))
	defer server.Close()
	owner := &scopeMemoryOwner{}
	client := newTestClient(t, HTTPClientConfig{MemoryAdmission: owner})
	err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error {
		if string(response.Body) != payload || owner.held != int64(cap(response.Body)) || response.InputMemory.State == platform.StateAvailable {
			return errors.New("chunked body lost capacity or fabricated mapping")
		}
		return nil
	})
	if err != nil || owner.held != 0 || owner.releases < 2 {
		t.Fatalf("actual chunked lifetime failed: %v owner=%+v", err, owner)
	}
}

func TestResponseScopeDecodedFailuresJoinEveryOwner(t *testing.T) {
	settlement := errors.New("decoder settlement failed")
	for _, test := range []struct {
		name       string
		encoding   string
		body       []byte
		releaseErr error
	}{
		{"corrupt", "gzip", []byte{0xff}, nil},
		{"expansion", "gzip", encodeResponseFixture(t, bytes.Repeat([]byte("x"), 8192), "gzip"), nil},
		{"unknown", "unknown", []byte("actual response"), nil},
		{"too_many_layers", "identity,identity,identity,identity,identity", []byte("actual response"), nil},
		{"settlement", "gzip", encodeResponseFixture(t, []byte("actual response"), "gzip"), settlement},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", test.encoding)
				w.Header().Set("Content-Length", fmt.Sprint(len(test.body)))
				if _, err := w.Write(test.body); err != nil {
					t.Logf("write rejected response: %v", err)
				}
			}))
			defer server.Close()
			owner := &scopeMemoryOwner{releaseErr: test.releaseErr}
			client := newTestClient(t, HTTPClientConfig{ChromeLike: true, MaxBodyBytes: 4096, MemoryAdmission: owner})
			called := false
			err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error { called = true; return nil })
			if err == nil || called || owner.held != 0 || owner.releases == 0 {
				t.Fatalf("invalid decode reached consumer or leaked ownership: %v called=%t owner=%+v", err, called, owner)
			}
			if test.releaseErr != nil && !errors.Is(err, test.releaseErr) {
				t.Fatalf("failed settlement lost its original cause: %v", err)
			}
		})
	}
}

func TestResponseScopePlainTransportOwnsGzipDecoder(t *testing.T) {
	payload := []byte("actual plain transport gzip response")
	encoded := encodeResponseFixture(t, payload, "gzip")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("plain transport gzip negotiation changed")
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", fmt.Sprint(len(encoded)))
		if _, err := w.Write(encoded); err != nil {
			t.Logf("write response after capacity rejection: %v", err)
		}
	}))
	defer server.Close()
	for _, capacity := range []int64{1 << 20, 2 << 20} {
		owner := &capacityResponseOwner{limit: capacity}
		client := newTestClient(t, HTTPClientConfig{MaxBodyBytes: 4096, MemoryAdmission: owner})
		called := false
		err := client.WithResponse(t.Context(), Request{URL: server.URL}, TargetNavigation, func(response *Response) error {
			called = true
			if !bytes.Equal(response.Body, payload) || owner.held != int64(len(encoded)+cap(response.Body)) {
				return errors.New("plain gzip escaped the explicit input/output owner")
			}
			return nil
		})
		if capacity == 1<<20 {
			if !errors.Is(err, errResponseCapacity) || called {
				t.Fatalf("transport-internal decoding bypassed workspace admission: %v", err)
			}
		} else if err != nil || !called {
			t.Fatalf("actual owned plain gzip failed: %v", err)
		}
		if owner.held != 0 {
			t.Fatal("plain gzip leaked admitted capacity")
		}
	}
}
