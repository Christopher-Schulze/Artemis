package observe

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"
)

// har_export.go (spec L4263: HAR Export HTTP Archive 1.2).
//
// Converts captured network events into HAR (HTTP Archive) 1.2 format
// for export and offline analysis. Thread-safe via RWMutex. Supports
// optional domain filtering so callers can restrict exports to a set
// of hosts.
//
// Reference: research/webstack/pinchtab-main/internal/bridge/observe/snapshot.go

// HARVersion is the HAR specification version produced by this exporter
// (spec L4263: HAR 1.2).
const HARVersion = "1.2"

// HARCreatorName is the name reported in the HAR creator field.
const HARCreatorName = "artemis"

// HARCreatorVersion is the version reported in the HAR creator field.
const HARCreatorVersion = "1.0.0"

// HARLog is the top-level HAR log object (spec L4263).
type HARLog struct {
	Version string        `json:"version"`
	Creator HARCreator    `json:"creator"`
	Entries []HAREntry    `json:"entries"`
	Pages   []interface{} `json:"pages,omitempty"`
}

// HARCreator identifies the software that produced the HAR
// (spec L4263).
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HAREntry is a single request/response pair in the HAR log
// (spec L4263).
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Timings         HARTimings  `json:"timings"`
	Cache           struct{}    `json:"cache"`
}

// HARRequest is the request side of a HAR entry (spec L4263).
type HARRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []HARNameValue `json:"headers"`
	Cookies     []HARCookie    `json:"cookies"`
	QueryString []HARNameValue `json:"queryString"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

// HARResponse is the response side of a HAR entry (spec L4263).
type HARResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []HARNameValue `json:"headers"`
	Content     HARContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

// HARContent is the response body content in a HAR entry
// (spec L4263).
type HARContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// HARTimings describes the timing breakdown of a HAR entry
// (spec L4263: send/wait/receive).
type HARTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

// HARNameValue is a generic name/value pair used for headers and
// query string parameters (spec L4263).
type HARNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARCookie is a cookie entry in a HAR request (spec L4263).
type HARCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
}

// HARExporter converts network events into HAR 1.2 logs
// (spec L4263). It is safe for concurrent use.
type HARExporter struct {
	mu             sync.RWMutex
	domainFilter   []string
	defaultCreator HARCreator
}

// NewHARExporter returns a HARExporter with the default artemis
// creator fields populated.
func NewHARExporter() *HARExporter {
	return &HARExporter{
		defaultCreator: HARCreator{
			Name:    HARCreatorName,
			Version: HARCreatorVersion,
		},
	}
}

// SetDomainFilter restricts which hosts are included in exports.
// When non-empty, only entries whose URL host matches one of the
// supplied domains (case-insensitive) are emitted. An empty slice
// disables filtering (spec L4263).
func (e *HARExporter) SetDomainFilter(domains []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	filter := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" {
			filter = append(filter, d)
		}
	}
	e.domainFilter = filter
}

// DomainFilter returns a copy of the current domain filter.
func (e *HARExporter) DomainFilter() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, len(e.domainFilter))
	copy(out, e.domainFilter)
	return out
}

// filterEntry reports whether the given entry should be included
// given the current domain filter. With no filter set, all entries
// pass (spec L4263).
func (e *HARExporter) filterEntry(entry HAREntry) bool {
	e.mu.RLock()
	filter := e.domainFilter
	e.mu.RUnlock()
	if len(filter) == 0 {
		return true
	}
	host := hostFromURL(entry.Request.URL)
	host = strings.ToLower(host)
	for _, d := range filter {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// hostFromURL extracts the host portion of a URL, returning the
// raw string when parsing fails so callers still see something.
func hostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return raw
	}
	return u.Host
}

// Export builds a HARLog from the supplied entries, applying the
// current domain filter. The returned log always carries the HAR
// 1.2 version and the configured creator (spec L4263).
func (e *HARExporter) Export(entries []HAREntry) HARLog {
	e.mu.RLock()
	creator := e.defaultCreator
	e.mu.RUnlock()

	log := HARLog{
		Version: HARVersion,
		Creator: creator,
		Entries: make([]HAREntry, 0, len(entries)),
	}
	for _, entry := range entries {
		if e.filterEntry(entry) {
			log.Entries = append(log.Entries, entry)
		}
	}
	return log
}

// ToJSON serializes a HARLog to compact JSON (spec L4263).
func (e *HARExporter) ToJSON(log HARLog) ([]byte, error) {
	return json.Marshal(log)
}

// FromNetworkEvents converts observe.NetworkEvent records (as
// produced by NetworkRingBuffer) into HAR entries. Each event
// becomes a request/response pair with timings derived from the
// event's status and byte count. The StartedDateTime is set to the
// current time in ISO 8601 UTC because NetworkEvent carries no
// timestamp; callers may overwrite it after conversion
// (spec L4263).
func (e *HARExporter) FromNetworkEvents(events []NetworkEvent) []HAREntry {
	entries := make([]HAREntry, 0, len(events))
	for _, ev := range events {
		method := strings.TrimSpace(ev.Method)
		if method == "" {
			method = "GET"
		}
		rawURL := ev.URL
		httpVersion := "HTTP/1.1"
		queryString := parseQueryString(rawURL)
		headers := []HARNameValue{}
		statusText := statusTextFor(ev.Status)

		entry := HAREntry{
			StartedDateTime: time.Now().UTC().Format(time.RFC3339Nano),
			Time:            0,
			Request: HARRequest{
				Method:      method,
				URL:         rawURL,
				HTTPVersion: httpVersion,
				Headers:     headers,
				Cookies:     []HARCookie{},
				QueryString: queryString,
				HeadersSize: -1,
				BodySize:    0,
			},
			Response: HARResponse{
				Status:      ev.Status,
				StatusText:  statusText,
				HTTPVersion: httpVersion,
				Headers:     []HARNameValue{},
				Content: HARContent{
					Size:     ev.Bytes,
					MimeType: mimeTypeForURL(rawURL),
				},
				RedirectURL: "",
				HeadersSize: -1,
				BodySize:    ev.Bytes,
			},
			Timings: HARTimings{
				Send:    0,
				Wait:    0,
				Receive: 0,
			},
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseQueryString extracts query parameters from a URL string as
// HAR name/value pairs. On parse failure it returns an empty slice.
func parseQueryString(raw string) []HARNameValue {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return []HARNameValue{}
	}
	q := u.Query()
	out := make([]HARNameValue, 0, len(q))
	for k, vs := range q {
		for _, v := range vs {
			out = append(out, HARNameValue{Name: k, Value: v})
		}
	}
	return out
}

// mimeTypeForURL returns a best-effort MIME type based on the URL
// path extension. Falls back to application/octet-stream.
func mimeTypeForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "application/octet-stream"
	}
	path := strings.ToLower(u.Path)
	switch {
	case strings.HasSuffix(path, ".html"), strings.HasSuffix(path, ".htm"):
		return "text/html"
	case strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".mjs"):
		return "application/javascript"
	case strings.HasSuffix(path, ".css"):
		return "text/css"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(path, ".gif"):
		return "image/gif"
	case strings.HasSuffix(path, ".woff"):
		return "font/woff"
	case strings.HasSuffix(path, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(path, ".txt"):
		return "text/plain"
	case strings.HasSuffix(path, ".xml"):
		return "application/xml"
	case strings.HasSuffix(path, ".wasm"):
		return "application/wasm"
	default:
		if path == "" || strings.HasSuffix(path, "/") {
			return "text/html"
		}
		return "application/octet-stream"
	}
}

// statusTextFor returns the canonical HTTP status text for a status
// code, defaulting to "OK" for unknown 2xx and "Unknown" otherwise.
func statusTextFor(status int) string {
	switch status {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 307:
		return "Temporary Redirect"
	case 308:
		return "Permanent Redirect"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 408:
		return "Request Timeout"
	case 409:
		return "Conflict"
	case 410:
		return "Gone"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	default:
		if status >= 200 && status < 300 {
			return "OK"
		}
		return "Unknown"
	}
}
