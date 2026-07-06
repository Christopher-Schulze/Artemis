package observe

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHARVersion_Is12(t *testing.T) {
	if HARVersion != "1.2" {
		t.Fatalf("expected HARVersion=1.2, got %s", HARVersion)
	}
}

func TestNewHARExporter_DefaultCreator(t *testing.T) {
	e := NewHARExporter()
	log := e.Export(nil)
	if log.Version != HARVersion {
		t.Fatalf("expected version=%s, got %s", HARVersion, log.Version)
	}
	if log.Creator.Name != HARCreatorName {
		t.Fatalf("expected creator name=%s, got %s", HARCreatorName, log.Creator.Name)
	}
	if log.Creator.Version != HARCreatorVersion {
		t.Fatalf("expected creator version=%s, got %s", HARCreatorVersion, log.Creator.Version)
	}
}

func TestHARExporter_ExportEmpty(t *testing.T) {
	e := NewHARExporter()
	log := e.Export(nil)
	if len(log.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(log.Entries))
	}
	if log.Version != "1.2" {
		t.Fatalf("expected version=1.2, got %s", log.Version)
	}
}

func TestHARExporter_ExportPreservesEntries(t *testing.T) {
	e := NewHARExporter()
	entries := []HAREntry{
		{
			StartedDateTime: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			Request: HARRequest{
				Method: "GET",
				URL:    "https://example.com/a",
			},
			Response: HARResponse{Status: 200},
		},
		{
			Request: HARRequest{
				Method: "POST",
				URL:    "https://example.com/b",
			},
			Response: HARResponse{Status: 201},
		},
	}
	log := e.Export(entries)
	if len(log.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(log.Entries))
	}
	if log.Entries[0].Request.Method != "GET" {
		t.Errorf("expected first method=GET, got %s", log.Entries[0].Request.Method)
	}
	if log.Entries[1].Response.Status != 201 {
		t.Errorf("expected second status=201, got %d", log.Entries[1].Response.Status)
	}
}

func TestHARExporter_ToJSON_Roundtrip(t *testing.T) {
	e := NewHARExporter()
	entries := []HAREntry{
		{
			StartedDateTime: "2024-01-01T12:00:00.000Z",
			Time:            42.5,
			Request: HARRequest{
				Method:      "GET",
				URL:         "https://example.com/page?x=1&y=2",
				HTTPVersion: "HTTP/1.1",
				QueryString: []HARNameValue{{Name: "x", Value: "1"}},
				HeadersSize: -1,
				BodySize:    0,
			},
			Response: HARResponse{
				Status:      200,
				StatusText:  "OK",
				HTTPVersion: "HTTP/1.1",
				Content:     HARContent{Size: 1024, MimeType: "text/html"},
				HeadersSize: -1,
				BodySize:    1024,
			},
			Timings: HARTimings{Send: 1, Wait: 30, Receive: 11.5},
		},
	}
	log := e.Export(entries)
	data, err := e.ToJSON(log)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}

	var parsed HARLog
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed.Version != "1.2" {
		t.Errorf("expected parsed version=1.2, got %s", parsed.Version)
	}
	if len(parsed.Entries) != 1 {
		t.Fatalf("expected 1 parsed entry, got %d", len(parsed.Entries))
	}
	if parsed.Entries[0].Request.Method != "GET" {
		t.Errorf("expected parsed method=GET, got %s", parsed.Entries[0].Request.Method)
	}
	if parsed.Entries[0].Response.Status != 200 {
		t.Errorf("expected parsed status=200, got %d", parsed.Entries[0].Response.Status)
	}
	if parsed.Entries[0].Timings.Wait != 30 {
		t.Errorf("expected parsed wait=30, got %f", parsed.Entries[0].Timings.Wait)
	}
}

func TestHARExporter_SetDomainFilter_FiltersEntries(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"example.com"})
	entries := []HAREntry{
		{Request: HARRequest{URL: "https://example.com/a"}},
		{Request: HARRequest{URL: "https://other.com/b"}},
		{Request: HARRequest{URL: "https://sub.example.com/c"}},
	}
	log := e.Export(entries)
	if len(log.Entries) != 2 {
		t.Fatalf("expected 2 entries after filter, got %d", len(log.Entries))
	}
	if log.Entries[0].Request.URL != "https://example.com/a" {
		t.Errorf("unexpected first url: %s", log.Entries[0].Request.URL)
	}
	if log.Entries[1].Request.URL != "https://sub.example.com/c" {
		t.Errorf("unexpected second url: %s", log.Entries[1].Request.URL)
	}
}

func TestHARExporter_SetDomainFilter_CaseInsensitive(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"EXAMPLE.com"})
	entries := []HAREntry{
		{Request: HARRequest{URL: "https://example.com/a"}},
		{Request: HARRequest{URL: "https://other.com/b"}},
	}
	log := e.Export(entries)
	if len(log.Entries) != 1 {
		t.Fatalf("expected 1 entry (case-insensitive), got %d", len(log.Entries))
	}
}

func TestHARExporter_SetDomainFilter_EmptyAllowsAll(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter(nil)
	entries := []HAREntry{
		{Request: HARRequest{URL: "https://a.com/"}},
		{Request: HARRequest{URL: "https://b.com/"}},
	}
	log := e.Export(entries)
	if len(log.Entries) != 2 {
		t.Fatalf("expected 2 entries with empty filter, got %d", len(log.Entries))
	}
}

func TestHARExporter_SetDomainFilter_TrimsAndDedupsBlanks(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"  example.com  ", "", "  "})
	filter := e.DomainFilter()
	if len(filter) != 1 {
		t.Fatalf("expected 1 filter entry, got %d", len(filter))
	}
	if filter[0] != "example.com" {
		t.Errorf("expected trimmed filter=example.com, got %s", filter[0])
	}
}

func TestHARExporter_DomainFilter_ReturnsCopy(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"example.com"})
	got := e.DomainFilter()
	got[0] = "mutated"
	again := e.DomainFilter()
	if again[0] != "example.com" {
		t.Errorf("DomainFilter did not return a copy: %s", again[0])
	}
}

func TestHARExporter_filterEntry_NoFilter(t *testing.T) {
	e := NewHARExporter()
	if !e.filterEntry(HAREntry{Request: HARRequest{URL: "https://anything.com/"}}) {
		t.Error("expected entry to pass with no filter")
	}
}

func TestHARExporter_filterEntry_SubdomainMatch(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"example.com"})
	if !e.filterEntry(HAREntry{Request: HARRequest{URL: "https://api.sub.example.com/x"}}) {
		t.Error("expected subdomain to match")
	}
	if e.filterEntry(HAREntry{Request: HARRequest{URL: "https://notexample.com/x"}}) {
		t.Error("expected notexample.com to NOT match")
	}
}

func TestHARExporter_FromNetworkEvents_Basic(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/page?x=1", Method: "GET", Status: 200, Bytes: 1024},
		{URL: "https://example.com/api", Method: "POST", Status: 201, Bytes: 64},
	}
	entries := e.FromNetworkEvents(events)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Request.Method != "GET" {
		t.Errorf("expected method=GET, got %s", entries[0].Request.Method)
	}
	if entries[0].Request.URL != "https://example.com/page?x=1" {
		t.Errorf("unexpected url: %s", entries[0].Request.URL)
	}
	if entries[0].Response.Status != 200 {
		t.Errorf("expected status=200, got %d", entries[0].Response.Status)
	}
	if entries[0].Response.BodySize != 1024 {
		t.Errorf("expected bodySize=1024, got %d", entries[0].Response.BodySize)
	}
	if entries[0].Response.Content.Size != 1024 {
		t.Errorf("expected content size=1024, got %d", entries[0].Response.Content.Size)
	}
}

func TestHARExporter_FromNetworkEvents_QueryString(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/page?a=1&b=2", Method: "GET", Status: 200, Bytes: 0},
	}
	entries := e.FromNetworkEvents(events)
	if len(entries[0].Request.QueryString) != 2 {
		t.Fatalf("expected 2 query params, got %d", len(entries[0].Request.QueryString))
	}
}

func TestHARExporter_FromNetworkEvents_EmptyMethodDefaultsGET(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/page", Method: "", Status: 200, Bytes: 0},
	}
	entries := e.FromNetworkEvents(events)
	if entries[0].Request.Method != "GET" {
		t.Fatalf("expected default method=GET, got %s", entries[0].Request.Method)
	}
}

func TestHARExporter_FromNetworkEvents_Empty(t *testing.T) {
	e := NewHARExporter()
	entries := e.FromNetworkEvents(nil)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestHARExporter_FromNetworkEvents_HTTPVersion(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/", Method: "GET", Status: 200, Bytes: 0},
	}
	entries := e.FromNetworkEvents(events)
	if entries[0].Request.HTTPVersion != "HTTP/1.1" {
		t.Errorf("expected HTTP/1.1, got %s", entries[0].Request.HTTPVersion)
	}
	if entries[0].Response.HTTPVersion != "HTTP/1.1" {
		t.Errorf("expected response HTTP/1.1, got %s", entries[0].Response.HTTPVersion)
	}
}

func TestHARExporter_FromNetworkEvents_StatusText(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/", Method: "GET", Status: 404, Bytes: 0},
		{URL: "https://example.com/", Method: "GET", Status: 500, Bytes: 0},
	}
	entries := e.FromNetworkEvents(events)
	if entries[0].Response.StatusText != "Not Found" {
		t.Errorf("expected 'Not Found', got %s", entries[0].Response.StatusText)
	}
	if entries[1].Response.StatusText != "Internal Server Error" {
		t.Errorf("expected 'Internal Server Error', got %s", entries[1].Response.StatusText)
	}
}

func TestMimeTypeForURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/index.html", "text/html"},
		{"https://example.com/app.js", "application/javascript"},
		{"https://example.com/style.css", "text/css"},
		{"https://example.com/data.json", "application/json"},
		{"https://example.com/logo.png", "image/png"},
		{"https://example.com/photo.jpg", "image/jpeg"},
		{"https://example.com/icon.svg", "image/svg+xml"},
		{"https://example.com/", "text/html"},
		{"https://example.com/unknown.xyz", "application/octet-stream"},
		{"https://example.com/font.woff2", "font/woff2"},
	}
	for _, tt := range tests {
		if got := mimeTypeForURL(tt.url); got != tt.want {
			t.Errorf("mimeTypeForURL(%q) = %s, want %s", tt.url, got, tt.want)
		}
	}
}

func TestStatusTextFor(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{200, "OK"},
		{201, "Created"},
		{204, "No Content"},
		{301, "Moved Permanently"},
		{302, "Found"},
		{304, "Not Modified"},
		{400, "Bad Request"},
		{401, "Unauthorized"},
		{403, "Forbidden"},
		{404, "Not Found"},
		{429, "Too Many Requests"},
		{500, "Internal Server Error"},
		{502, "Bad Gateway"},
		{503, "Service Unavailable"},
		{504, "Gateway Timeout"},
		{250, "OK"}, // unknown 2xx
		{700, "Unknown"},
		{0, "Unknown"},
	}
	for _, tt := range tests {
		if got := statusTextFor(tt.status); got != tt.want {
			t.Errorf("statusTextFor(%d) = %s, want %s", tt.status, got, tt.want)
		}
	}
}

func TestHARExporter_FromNetworkEvents_ThenExport(t *testing.T) {
	e := NewHARExporter()
	e.SetDomainFilter([]string{"example.com"})
	events := []NetworkEvent{
		{URL: "https://example.com/a", Method: "GET", Status: 200, Bytes: 10},
		{URL: "https://other.com/b", Method: "GET", Status: 200, Bytes: 20},
	}
	entries := e.FromNetworkEvents(events)
	log := e.Export(entries)
	if len(log.Entries) != 1 {
		t.Fatalf("expected 1 entry after filter, got %d", len(log.Entries))
	}
	if !strings.Contains(log.Entries[0].Request.URL, "example.com") {
		t.Errorf("unexpected url: %s", log.Entries[0].Request.URL)
	}
}

func TestHARExporter_FromNetworkEvents_JSONSerializable(t *testing.T) {
	e := NewHARExporter()
	events := []NetworkEvent{
		{URL: "https://example.com/page?x=1", Method: "GET", Status: 200, Bytes: 100},
	}
	entries := e.FromNetworkEvents(events)
	log := e.Export(entries)
	data, err := e.ToJSON(log)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if !strings.Contains(string(data), "\"version\":\"1.2\"") {
		t.Errorf("expected version 1.2 in JSON: %s", string(data))
	}
}

func TestHARExporter_ConcurrentExportAndFilter(t *testing.T) {
	e := NewHARExporter()
	entries := make([]HAREntry, 100)
	for i := range entries {
		entries[i] = HAREntry{Request: HARRequest{URL: "https://example.com/x"}}
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Export(entries)
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.SetDomainFilter([]string{"example.com"})
			_ = e.DomainFilter()
		}()
	}
	wg.Wait()
}
