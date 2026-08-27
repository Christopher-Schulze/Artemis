package observe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sampleNetworkEvents returns a deterministic multi-event fixture used by the
// export tests.
func sampleNetworkEvents() []NetworkEvent {
	start := time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC)
	return []NetworkEvent{
		{RequestID: "r1", URL: "https://fixture.test/a", Method: "GET", Status: 200, Bytes: 128, Start: start, DurationMS: 12},
		{RequestID: "r2", URL: "https://fixture.test/b", Method: "POST", Status: 201, Bytes: 64, Start: start.Add(time.Second), DurationMS: 30},
		{RequestID: "r3", URL: "https://fixture.test/c", Method: "GET", Status: 404, Bytes: 0, Start: start.Add(2 * time.Second), DurationMS: 5},
	}
}

// TestFormatOutputHAREncodesEveryEvent is the mutation proof for the HAR
// drop-bug: FormatOutput previously allocated an empty entry slice from
// len(events) and never filled it, so every HAR export returned an empty log.
func TestFormatOutputHAREncodesEveryEvent(t *testing.T) {
	events := sampleNetworkEvents()

	encoded, err := FormatOutput(OutputFormatHAR, events)
	if err != nil {
		t.Fatalf("FormatOutput HAR: %v", err)
	}
	var document HARDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("unmarshal HAR: %v", err)
	}
	log := document.Log
	if log.Version != HARVersion || log.Creator.Name != HARCreatorName {
		t.Fatalf("HAR envelope version=%q creator=%q", log.Version, log.Creator.Name)
	}
	if len(log.Entries) != len(events) {
		t.Fatalf("HAR entries = %d, want %d", len(log.Entries), len(events))
	}
	for i, entry := range log.Entries {
		if entry.Request.URL != events[i].URL {
			t.Fatalf("entry %d URL = %q, want %q", i, entry.Request.URL, events[i].URL)
		}
		if entry.Response.Status != events[i].Status {
			t.Fatalf("entry %d status = %d, want %d", i, entry.Response.Status, events[i].Status)
		}
	}
}

// TestFormatOutputNDJSONEmitsOneLinePerEvent proves the NDJSON path keeps one
// decodable object per line.
func TestFormatOutputNDJSONEmitsOneLinePerEvent(t *testing.T) {
	events := sampleNetworkEvents()

	encoded, err := FormatOutput(OutputFormatNDJSON, events)
	if err != nil {
		t.Fatalf("FormatOutput NDJSON: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(encoded), "\n"), "\n")
	if len(lines) != len(events) {
		t.Fatalf("NDJSON lines = %d, want %d", len(lines), len(events))
	}
	for i, line := range lines {
		var decoded NetworkEvent
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if decoded.RequestID != events[i].RequestID {
			t.Fatalf("line %d requestId = %q, want %q", i, decoded.RequestID, events[i].RequestID)
		}
	}
}

// TestFromNetworkEventsMapsCapturedFields proves the exporter carries the
// captured evidence instead of fabricating it: start time, duration, both
// header sets, post data and MIME type all come from the event.
func TestFromNetworkEventsMapsCapturedFields(t *testing.T) {
	start := time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC)
	event := NetworkEvent{
		RequestID:       "r1",
		URL:             "https://fixture.test/submit?tab=1",
		Method:          "POST",
		Status:          200,
		MimeType:        "application/json",
		Headers:         map[string]string{"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
		ResponseHeaders: map[string]string{"Content-Length": "17", "Server": "fixture"},
		PostData:        "field=value&x=1",
		Bytes:           17,
		Start:           start,
		DurationMS:      42,
	}

	entries := NewHARExporter().FromNetworkEvents([]NetworkEvent{event})
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0]

	if want := start.Format(time.RFC3339Nano); entry.StartedDateTime != want {
		t.Fatalf("startedDateTime = %q, want %q", entry.StartedDateTime, want)
	}
	if entry.Time != 42 {
		t.Fatalf("time = %v, want 42", entry.Time)
	}
	if entry.Response.Content.MimeType != "application/json" {
		t.Fatalf("mimeType = %q, want the captured application/json", entry.Response.Content.MimeType)
	}
	if entry.Request.BodySize != len(event.PostData) {
		t.Fatalf("request bodySize = %d, want %d", entry.Request.BodySize, len(event.PostData))
	}
	if entry.Request.PostData == nil {
		t.Fatal("post data missing from exported entry")
	}
	if entry.Request.PostData.Text != event.PostData {
		t.Fatalf("post data text = %q, want %q", entry.Request.PostData.Text, event.PostData)
	}
	if entry.Request.PostData.MimeType != "application/x-www-form-urlencoded" {
		t.Fatalf("post data mimeType = %q, want the request content type", entry.Request.PostData.MimeType)
	}
	if got := headerValue(entry.Request.Headers, "Accept"); got != "application/json" {
		t.Fatalf("request Accept header = %q, want application/json", got)
	}
	if got := headerValue(entry.Response.Headers, "Server"); got != "fixture" {
		t.Fatalf("response Server header = %q, want fixture", got)
	}
	if len(entry.Request.QueryString) != 1 || entry.Request.QueryString[0].Name != "tab" {
		t.Fatalf("query string = %+v, want a single tab parameter", entry.Request.QueryString)
	}
	if entry.Timings.Send+entry.Timings.Wait+entry.Timings.Receive != entry.Time {
		t.Fatalf("timings %+v do not sum to time %v", entry.Timings, entry.Time)
	}
}

// TestFromNetworkEventsSortsHeadersDeterministically proves exported archives
// are byte-stable across runs even though headers are captured in a map.
func TestFromNetworkEventsSortsHeadersDeterministically(t *testing.T) {
	event := NetworkEvent{
		URL:     "https://fixture.test/",
		Headers: map[string]string{"Zeta": "1", "Alpha": "2", "Mike": "3", "Bravo": "4"},
	}
	exporter := NewHARExporter()

	first := exporter.FromNetworkEvents([]NetworkEvent{event})[0].Request.Headers
	want := []string{"Alpha", "Bravo", "Mike", "Zeta"}
	for i, name := range want {
		if first[i].Name != name {
			t.Fatalf("header %d = %q, want %q", i, first[i].Name, name)
		}
	}
	for run := 0; run < 32; run++ {
		again := exporter.FromNetworkEvents([]NetworkEvent{event})[0].Request.Headers
		for i := range first {
			if again[i] != first[i] {
				t.Fatalf("run %d header %d = %+v, want %+v", run, i, again[i], first[i])
			}
		}
	}
}

// TestFromNetworkEventsRedactsCredentialHeaders proves an exported archive
// cannot leak a session on either header side.
func TestFromNetworkEventsRedactsCredentialHeaders(t *testing.T) {
	event := NetworkEvent{
		URL:             "https://fixture.test/",
		Headers:         map[string]string{"Authorization": "Bearer secret-token", "X-Api-Key": "key-123", "Accept": "*/*"},
		ResponseHeaders: map[string]string{"Set-Cookie": "session=abc", "Server": "fixture"},
	}

	entry := NewHARExporter().FromNetworkEvents([]NetworkEvent{event})[0]

	for _, check := range []struct {
		side   string
		pairs  []HARNameValue
		name   string
		secret string
	}{
		{"request", entry.Request.Headers, "Authorization", "Bearer secret-token"},
		{"request", entry.Request.Headers, "X-Api-Key", "key-123"},
		{"response", entry.Response.Headers, "Set-Cookie", "session=abc"},
	} {
		got := headerValue(check.pairs, check.name)
		if got != redactedHeaderValue {
			t.Fatalf("%s %s = %q, want %q", check.side, check.name, got, redactedHeaderValue)
		}
		if got == check.secret {
			t.Fatalf("%s %s leaked its secret", check.side, check.name)
		}
	}
	if got := headerValue(entry.Request.Headers, "Accept"); got != "*/*" {
		t.Fatalf("non-sensitive header was redacted: %q", got)
	}
}

// TestFromNetworkEventsZeroStartLeavesTimestampEmpty proves the exporter does
// not fabricate a capture time for an event that never started.
func TestFromNetworkEventsZeroStartLeavesTimestampEmpty(t *testing.T) {
	entry := NewHARExporter().FromNetworkEvents([]NetworkEvent{{URL: "https://fixture.test/"}})[0]
	if entry.StartedDateTime != "" {
		t.Fatalf("startedDateTime = %q, want empty for a zero start time", entry.StartedDateTime)
	}
	if entry.Time != 0 || entry.Timings != (HARTimings{}) {
		t.Fatalf("time = %v timings = %+v, want zero for an unmeasured event", entry.Time, entry.Timings)
	}
}

// TestFromNetworkEventsFallsBackToURLMimeType proves URL-based guessing is
// only a fallback and never overrides the captured MIME type.
func TestFromNetworkEventsFallsBackToURLMimeType(t *testing.T) {
	tests := []struct {
		name     string
		event    NetworkEvent
		wantMime string
	}{
		{"captured mime wins", NetworkEvent{URL: "https://fixture.test/data.png", MimeType: "application/json"}, "application/json"},
		{"url fallback", NetworkEvent{URL: "https://fixture.test/data.png"}, "image/png"},
		{"blank mime falls back", NetworkEvent{URL: "https://fixture.test/app.css", MimeType: "  "}, "text/css"},
	}
	exporter := NewHARExporter()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := exporter.FromNetworkEvents([]NetworkEvent{tc.event})[0]
			if entry.Response.Content.MimeType != tc.wantMime {
				t.Fatalf("mimeType = %q, want %q", entry.Response.Content.MimeType, tc.wantMime)
			}
		})
	}
}

// TestHARTimingsBreakdown pins the send/wait/receive split.
func TestHARTimingsBreakdown(t *testing.T) {
	tests := []struct {
		name       string
		durationMS float64
		want       HARTimings
	}{
		{"unmeasured", 0, HARTimings{}},
		{"negative", -5, HARTimings{}},
		{"sub-millisecond split collapses to wait", 2, HARTimings{Send: 0, Wait: 2, Receive: 0}},
		{"split at the boundary", 3, HARTimings{Send: 1, Wait: 1, Receive: 1}},
		{"normal transfer", 42, HARTimings{Send: 1, Wait: 40, Receive: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := harTimings(tc.durationMS); got != tc.want {
				t.Fatalf("harTimings(%v) = %+v, want %+v", tc.durationMS, got, tc.want)
			}
		})
	}
}

// TestLiveCollectorKeepsRequestAndResponseHeaders is the regression proof for
// the capture-side defect: the response event used to overwrite the request
// headers, so a finished request kept only one of the two sets.
func TestLiveCollectorKeepsRequestAndResponseHeaders(t *testing.T) {
	collector, err := NewLiveCollector(liveCaller{}, nil, DefaultLiveConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer closeObserveTestResource(t, "live collector", collector.Close)

	collector.recordEvent(bridgeEvent("Network.requestWillBeSent", map[string]any{
		"requestId": "r1", "type": "Document", "request": map[string]any{
			"url": "https://fixture.test/index", "method": "GET",
			"headers": map[string]string{"Accept": "text/html", "X-Trace": "abc"},
		},
	}))
	collector.recordEvent(bridgeEvent("Network.responseReceived", map[string]any{
		"requestId": "r1", "response": map[string]any{
			"url": "https://fixture.test/index", "status": 200, "mimeType": "text/html",
			"headers": map[string]string{"Server": "fixture"},
		},
	}))
	collector.recordEvent(bridgeEvent("Network.loadingFinished", map[string]any{"requestId": "r1", "encodedDataLength": 64}))

	evidence, err := collector.CaptureEvidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var captured *NetworkEvent
	for i := range evidence.Network {
		if evidence.Network[i].RequestID == "r1" {
			captured = &evidence.Network[i]
		}
	}
	if captured == nil {
		t.Fatal("request r1 missing from evidence")
	}
	if captured.Headers["Accept"] != "text/html" || captured.Headers["X-Trace"] != "abc" {
		t.Fatalf("request headers lost: %+v", captured.Headers)
	}
	if captured.ResponseHeaders["Server"] != "fixture" {
		t.Fatalf("response headers missing: %+v", captured.ResponseHeaders)
	}
}

// headerValue returns the value of a named HAR header pair, or the empty
// string when the pair is absent.
func headerValue(pairs []HARNameValue, name string) string {
	for _, pair := range pairs {
		if pair.Name == name {
			return pair.Value
		}
	}
	return ""
}
