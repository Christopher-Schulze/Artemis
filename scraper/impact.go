// Package scraper implements server impact detection (spec ss28.12b.12).
//
// Server Impact Detection (scraper/impact.go):
// - Response time >2x baseline -> halve rate
// - HTTP 429 -> 50% rate, respect Retry-After
// - HTTP 503 -> 60s pause, 25% rate
// - Connection refused -> 5min pause
// - Timeout rate >20% -> halve rate
package scraper

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ImpactAction describes the action to take based on server impact.
type ImpactAction int

const (
	ImpactNone        ImpactAction = iota // no action needed
	ImpactHalveRate                       // halve the request rate
	ImpactQuarterRate                     // reduce to 25% rate
	ImpactPause60s                        // pause for 60 seconds
	ImpactPause5min                       // pause for 5 minutes
)

// String returns the action name.
func (a ImpactAction) String() string {
	switch a {
	case ImpactNone:
		return "none"
	case ImpactHalveRate:
		return "halve_rate"
	case ImpactQuarterRate:
		return "quarter_rate"
	case ImpactPause60s:
		return "pause_60s"
	case ImpactPause5min:
		return "pause_5min"
	default:
		return "unknown"
	}
}

// ImpactEvent records a single response for impact analysis.
type ImpactEvent struct {
	Host          string
	StatusCode    int
	ResponseTime  time.Duration
	TimedOut      bool
	ConnectionErr bool
	RetryAfter    time.Duration // from Retry-After header, 0 if absent
	Timestamp     time.Time
}

// ImpactDecision is the result of analyzing an impact event.
type ImpactDecision struct {
	Action     ImpactAction
	PauseUntil time.Time
	RateFactor float64 // 1.0 = normal, 0.5 = halved, 0.25 = quartered
	Reason     string
}

// ImpactDetector tracks per-host response patterns and recommends
// rate adjustments based on server impact signals.
type ImpactDetector struct {
	mu           sync.Mutex
	baselines    map[string]time.Duration // host -> baseline response time
	pauseUntil   map[string]time.Time     // host -> pause deadline
	rateFactor   map[string]float64       // host -> current rate factor
	timeoutCount map[string]int           // host -> consecutive timeout count
	totalCount   map[string]int           // host -> total request count
}

// NewImpactDetector creates an empty impact detector.
func NewImpactDetector() *ImpactDetector {
	return &ImpactDetector{
		baselines:    make(map[string]time.Duration),
		pauseUntil:   make(map[string]time.Time),
		rateFactor:   make(map[string]float64),
		timeoutCount: make(map[string]int),
		totalCount:   make(map[string]int),
	}
}

// SetBaseline sets the baseline response time for a host.
// Used for the >2x baseline detection rule.
func (d *ImpactDetector) SetBaseline(host string, baseline time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baselines[host] = baseline
	if d.rateFactor[host] == 0 {
		d.rateFactor[host] = 1.0
	}
}

// Analyze evaluates an impact event and returns a decision.
// The decision tells the caller what action to take (halve rate,
// pause, etc.) and when to resume.
func (d *ImpactDetector) Analyze(event ImpactEvent) ImpactDecision {
	d.mu.Lock()
	defer d.mu.Unlock()

	host := event.Host
	if d.rateFactor[host] == 0 {
		d.rateFactor[host] = 1.0
	}
	d.totalCount[host]++

	// Rule: Connection refused -> 5min pause
	if event.ConnectionErr {
		d.pauseUntil[host] = event.Timestamp.Add(5 * time.Minute)
		d.rateFactor[host] = 0.25
		return ImpactDecision{
			Action:     ImpactPause5min,
			PauseUntil: d.pauseUntil[host],
			RateFactor: 0.25,
			Reason:     "connection refused",
		}
	}

	// Rule: HTTP 503 -> 60s pause, 25% rate
	if event.StatusCode == 503 {
		d.pauseUntil[host] = event.Timestamp.Add(60 * time.Second)
		d.rateFactor[host] = 0.25
		return ImpactDecision{
			Action:     ImpactPause60s,
			PauseUntil: d.pauseUntil[host],
			RateFactor: 0.25,
			Reason:     "HTTP 503 service unavailable",
		}
	}

	// Rule: HTTP 429 -> 50% rate, respect Retry-After
	if event.StatusCode == 429 {
		pause := event.RetryAfter
		if pause <= 0 {
			pause = 30 * time.Second // default if no Retry-After
		}
		d.pauseUntil[host] = event.Timestamp.Add(pause)
		d.rateFactor[host] = 0.5
		return ImpactDecision{
			Action:     ImpactPause60s,
			PauseUntil: d.pauseUntil[host],
			RateFactor: 0.5,
			Reason:     "HTTP 429 rate limited",
		}
	}

	// Rule: Timeout -> track timeout rate
	if event.TimedOut {
		d.timeoutCount[host]++
		timeoutRate := float64(d.timeoutCount[host]) / float64(d.totalCount[host])
		if timeoutRate > 0.20 {
			d.rateFactor[host] = d.rateFactor[host] * 0.5
			return ImpactDecision{
				Action:     ImpactHalveRate,
				RateFactor: d.rateFactor[host],
				Reason:     "timeout rate >20%",
			}
		}
		return ImpactDecision{
			Action:     ImpactNone,
			RateFactor: d.rateFactor[host],
			Reason:     "timeout but rate within threshold",
		}
	}

	// Not a timeout - reset timeout counter on success
	if event.StatusCode >= 200 && event.StatusCode < 400 {
		d.timeoutCount[host] = 0
	}

	// Rule: Response time >2x baseline -> halve rate
	baseline := d.baselines[host]
	if baseline > 0 && event.ResponseTime > 2*baseline {
		d.rateFactor[host] = d.rateFactor[host] * 0.5
		return ImpactDecision{
			Action:     ImpactHalveRate,
			RateFactor: d.rateFactor[host],
			Reason:     "response time >2x baseline",
		}
	}

	// Recovery: gradually restore rate on healthy responses
	if event.StatusCode >= 200 && event.StatusCode < 300 {
		if d.rateFactor[host] < 1.0 {
			d.rateFactor[host] = d.rateFactor[host] + 0.1
			if d.rateFactor[host] > 1.0 {
				d.rateFactor[host] = 1.0
			}
		}
	}

	return ImpactDecision{
		Action:     ImpactNone,
		RateFactor: d.rateFactor[host],
		Reason:     "healthy",
	}
}

// IsPaused returns true if the host is currently in a pause period.
func (d *ImpactDetector) IsPaused(host string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	until, ok := d.pauseUntil[host]
	if !ok {
		return false
	}
	return now.Before(until)
}

// RateFactor returns the current rate factor for a host (1.0 = normal).
func (d *ImpactDetector) RateFactor(host string) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	rf := d.rateFactor[host]
	if rf == 0 {
		return 1.0
	}
	return rf
}

// ParseRetryAfter parses the Retry-After header value into a duration.
// Supports both delta-seconds and HTTP-date formats.
func ParseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	// Try delta-seconds
	if secs, err := strconv.Atoi(header); err == nil {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date
	if t, err := http.ParseTime(header); err == nil {
		return time.Until(t)
	}
	return 0
}

// EventFromResponse builds an ImpactEvent from an HTTP response.
func EventFromResponse(host string, statusCode int, responseTime time.Duration, headers http.Header, timedOut, connErr bool, now time.Time) ImpactEvent {
	event := ImpactEvent{
		Host:          host,
		StatusCode:    statusCode,
		ResponseTime:  responseTime,
		TimedOut:      timedOut,
		ConnectionErr: connErr,
		Timestamp:     now,
	}
	if headers != nil {
		event.RetryAfter = ParseRetryAfter(headers.Get("Retry-After"))
	}
	return event
}
