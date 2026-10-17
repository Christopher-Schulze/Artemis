package bridge

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func newResolutionTestOwner(t *testing.T) (*fetchResolver, *network.Policy) {
	t.Helper()
	page := &Page{sessionID: "owner"}
	job := fetchRequestJob{sessionID: "owner"}
	job.payload.RequestID = "request"
	job.payload.ResourceType = "Document"
	job.payload.Request.URL = "http://127.0.0.1:8080/"
	job.payload.Request.Method = "POST"
	job.payload.Request.PostData = "é"
	job.payload.Request.HasPostData = true
	job.payload.Request.Headers = map[string]any{"content-type": "text/plain", "content-length": "2"}
	owner, err := page.newFetchResolver(job)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := network.NewPolicy(network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{8080}, MaxRequestBodyBytes: 4, MaxResponseBodyBytes: 64}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, policy
}

func TestFetchContinuationAdmitsActualFrozenTargetHeadersAndBytes(t *testing.T) {
	for _, test := range []struct {
		name       string
		resolution FetchResolution
		deny       bool
	}{
		{"inherit", FetchResolution{Kind: FetchResolutionContinue}, false},
		{"UTF8 bytes", FetchResolution{Kind: FetchResolutionContinue, PostData: base64.StdEncoding.EncodeToString([]byte("éé")), Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}}}, false},
		{"denied URL", FetchResolution{Kind: FetchResolutionContinue, URL: "http://127.0.0.1:9090/"}, true},
		{"denied method", FetchResolution{Kind: FetchResolutionContinue, Method: "TRACE"}, true},
		{"unbounded actual body", FetchResolution{Kind: FetchResolutionContinue, PostData: base64.StdEncoding.EncodeToString([]byte("ééé")), Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}}}, true},
		{"non-base64 body", FetchResolution{Kind: FetchResolutionContinue, PostData: "plain-text"}, true},
		{"noncanonical base64", FetchResolution{Kind: FetchResolutionContinue, PostData: "Zh=="}, true},
		{"false length", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "Content-Length", Value: "1"}}}, true},
		{"empty length", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "Content-Length", Value: ""}}}, true},
		{"removed headers retain body extent", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{}}, true},
		{"Host override", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Host", Value: "evil.example"}}}, true},
		{"case collision", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "content-type", Value: "application/json"}}}, true},
		{"header injection", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "X-Fake", Value: "value\nInjected: yes"}}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, policy := newResolutionTestOwner(t)
			copy := owner.requestCopy()
			copy.URL = "http://evil.example/"
			copy.Method = "DELETE"
			copy.Headers["Content-Type"] = "application/octet-stream"
			if _, err := owner.validate(t.Context(), policy, test.resolution); errors.Is(err, network.ErrPolicyDenied) != test.deny || err != nil && !test.deny {
				t.Fatalf("final admission deny=%t error=%v", test.deny, err)
			}
			if owner.request.URL != "http://127.0.0.1:8080/" || owner.request.Method != "POST" || owner.request.Headers["Content-Type"] != "text/plain" {
				t.Fatal("application mutated admitted source")
			}
		})
	}
}

func TestFetchFulfillmentBoundsActualDecodedResponse(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(strings.Repeat("x", 512))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		resolution FetchResolution
		deny       bool
	}{
		{"local response", FetchResolution{Kind: FetchResolutionFulfill, Body: "actual-local-body", ResponseHeaders: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}}}, false},
		{"wire bytes", FetchResolution{Kind: FetchResolutionFulfill, Body: strings.Repeat("x", 65)}, true},
		{"decoded expansion", FetchResolution{Kind: FetchResolutionFulfill, Body: compressed.String(), ResponseHeaders: []FetchHeader{{Name: "Content-Encoding", Value: "gzip"}}}, true},
		{"false length", FetchResolution{Kind: FetchResolutionFulfill, Body: "value", ResponseHeaders: []FetchHeader{{Name: "Content-Length", Value: "1"}}}, true},
		{"status", FetchResolution{Kind: FetchResolutionFulfill, StatusCode: 101}, true},
		{"bodyless status", FetchResolution{Kind: FetchResolutionFulfill, StatusCode: 204, Body: "value"}, true},
		{"chunked override", FetchResolution{Kind: FetchResolutionFulfill, ResponseHeaders: []FetchHeader{{Name: "Transfer-Encoding", Value: "chunked"}}}, true},
		{"invalid kind", FetchResolution{Kind: FetchResolutionKind("invented")}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, policy := newResolutionTestOwner(t)
			if _, err := owner.validate(t.Context(), policy, test.resolution); errors.Is(err, network.ErrPolicyDenied) != test.deny || err != nil && !test.deny {
				t.Fatalf("response admission deny=%t error=%v", test.deny, err)
			}
		})
	}
}

func TestFetchHeaderOverridesDistinguishAbsentAndEmptyOnWire(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers []FetchHeader
		want    string
	}{
		{"absent", nil, `{"requestId":"request"}`},
		{"explicit empty", []FetchHeader{}, `{"requestId":"request","headers":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, params, err := fetchResolutionCommand("request", FetchResolution{Kind: FetchResolutionContinue, Headers: test.headers})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(params)
			if err != nil || string(wire) != test.want {
				t.Fatalf("wire=%s error=%v", wire, err)
			}
		})
	}
}

func TestNativeRequestLengthCannotTrustAContradictoryHeader(t *testing.T) {
	for _, test := range []struct {
		name, value, body string
		want              int64
	}{
		{"smaller", "1", "é", -1}, {"negative", "-1", "", -1}, {"invalid", "garbage", "", -1}, {"matching", "2", "é", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := fetchContentLength(map[string]any{"Content-Length": test.value}, test.body, true); got != test.want {
				t.Fatalf("length=%d want=%d", got, test.want)
			}
		})
	}
}

func TestValidatedHeadersRemainDetachedAndBounded(t *testing.T) {
	source := http.Header{"X-Source": []string{"before"}}
	copy, err := network.ValidatedHeaders(source)
	if err != nil {
		t.Fatal(err)
	}
	source["X-Source"][0] = "after"
	if copy.Get("X-Source") != "before" {
		t.Fatal("header snapshot aliases application input")
	}
	large := make(http.Header)
	for index := 0; index < 129; index++ {
		large[fmt.Sprintf("X-Field-%03d", index)] = nil
	}
	if _, err := network.ValidatedHeaders(large); !errors.Is(err, network.ErrPolicyDenied) {
		t.Fatalf("header count not bounded: %v", err)
	}
	values := make([]string, 129)
	for index := range values {
		values[index] = "valid"
	}
	total := make(http.Header)
	for index := range 9 {
		total[fmt.Sprintf("X-Field-%03d", index)] = []string{strings.Repeat("x", 4096)}
	}
	for _, test := range []struct {
		name    string
		headers http.Header
	}{
		{"name bytes", http.Header{strings.Repeat("x", 257): []string{"valid"}}},
		{"value count", http.Header{"X-Values": values}},
		{"value bytes", http.Header{"X-Value": []string{strings.Repeat("x", 4097)}}},
		{"total bytes", total},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := network.ValidatedHeaders(test.headers); !errors.Is(err, network.ErrPolicyDenied) {
				t.Fatalf("header budget admitted: %v", err)
			}
		})
	}
}
