package scraper

import (
	"net/http"
	"testing"
	"time"
)

func TestImpactDetectorConnectionRefused(t *testing.T) {
	d := NewImpactDetector()
	event := ImpactEvent{
		Host:          "example.com",
		ConnectionErr: true,
		Timestamp:     time.Now(),
	}
	decision := d.Analyze(event)
	if decision.Action != ImpactPause5min {
		t.Fatalf("expected pause_5min, got %s", decision.Action)
	}
	if decision.RateFactor != 0.25 {
		t.Fatalf("expected 0.25 rate, got %f", decision.RateFactor)
	}
}

func TestImpactDetectorHTTP503(t *testing.T) {
	d := NewImpactDetector()
	event := ImpactEvent{
		Host:       "example.com",
		StatusCode: 503,
		Timestamp:  time.Now(),
	}
	decision := d.Analyze(event)
	if decision.Action != ImpactPause60s {
		t.Fatalf("expected pause_60s, got %s", decision.Action)
	}
	if decision.RateFactor != 0.25 {
		t.Fatalf("expected 0.25 rate, got %f", decision.RateFactor)
	}
}

func TestImpactDetectorHTTP429(t *testing.T) {
	d := NewImpactDetector()
	event := ImpactEvent{
		Host:       "example.com",
		StatusCode: 429,
		RetryAfter: 10 * time.Second,
		Timestamp:  time.Now(),
	}
	decision := d.Analyze(event)
	if decision.Action != ImpactPause60s {
		t.Fatalf("expected pause_60s, got %s", decision.Action)
	}
	if decision.RateFactor != 0.5 {
		t.Fatalf("expected 0.5 rate, got %f", decision.RateFactor)
	}
}

func TestImpactDetectorHTTP429NoRetryAfter(t *testing.T) {
	d := NewImpactDetector()
	event := ImpactEvent{
		Host:       "example.com",
		StatusCode: 429,
		Timestamp:  time.Now(),
	}
	decision := d.Analyze(event)
	if decision.Action != ImpactPause60s {
		t.Fatalf("expected pause, got %s", decision.Action)
	}
}

func TestImpactDetectorResponseTime2xBaseline(t *testing.T) {
	d := NewImpactDetector()
	d.SetBaseline("example.com", 500*time.Millisecond)
	event := ImpactEvent{
		Host:         "example.com",
		StatusCode:   200,
		ResponseTime: 1200 * time.Millisecond, // >2x baseline
		Timestamp:    time.Now(),
	}
	decision := d.Analyze(event)
	if decision.Action != ImpactHalveRate {
		t.Fatalf("expected halve_rate, got %s", decision.Action)
	}
}

func TestImpactDetectorTimeoutRate20Percent(t *testing.T) {
	d := NewImpactDetector()
	// 3 timeouts out of 10 = 30% > 20%
	for i := 0; i < 7; i++ {
		d.Analyze(ImpactEvent{Host: "example.com", StatusCode: 200, Timestamp: time.Now()})
	}
	for i := 0; i < 3; i++ {
		decision := d.Analyze(ImpactEvent{Host: "example.com", TimedOut: true, Timestamp: time.Now()})
		if i == 2 { // third timeout triggers >20%
			if decision.Action != ImpactHalveRate {
				t.Fatalf("expected halve_rate on 3rd timeout, got %s", decision.Action)
			}
		}
	}
}

func TestImpactDetectorHealthyRecovery(t *testing.T) {
	d := NewImpactDetector()
	d.SetBaseline("example.com", 500*time.Millisecond)
	// Trigger a halve
	d.Analyze(ImpactEvent{
		Host:         "example.com",
		StatusCode:   200,
		ResponseTime: 1200 * time.Millisecond,
		Timestamp:    time.Now(),
	})
	rf := d.RateFactor("example.com")
	if rf >= 1.0 {
		t.Fatalf("expected rate <1.0 after halve, got %f", rf)
	}
	// Healthy responses should gradually restore
	for i := 0; i < 10; i++ {
		d.Analyze(ImpactEvent{
			Host:         "example.com",
			StatusCode:   200,
			ResponseTime: 300 * time.Millisecond,
			Timestamp:    time.Now(),
		})
	}
	rf = d.RateFactor("example.com")
	if rf < 1.0 {
		t.Fatalf("expected rate restored to 1.0, got %f", rf)
	}
}

func TestImpactDetectorIsPaused(t *testing.T) {
	d := NewImpactDetector()
	now := time.Now()
	event := ImpactEvent{
		Host:       "example.com",
		StatusCode: 503,
		Timestamp:  now,
	}
	d.Analyze(event)
	if !d.IsPaused("example.com", now.Add(30*time.Second)) {
		t.Fatal("expected to be paused at +30s")
	}
	if d.IsPaused("example.com", now.Add(61*time.Second)) {
		t.Fatal("expected not paused at +61s")
	}
}

func TestImpactDetectorNotPausedForHealthy(t *testing.T) {
	d := NewImpactDetector()
	d.Analyze(ImpactEvent{Host: "example.com", StatusCode: 200, Timestamp: time.Now()})
	if d.IsPaused("example.com", time.Now()) {
		t.Fatal("expected not paused for healthy response")
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	dur := ParseRetryAfter("120")
	if dur != 120*time.Second {
		t.Fatalf("expected 120s, got %v", dur)
	}
}

func TestParseRetryAfterEmpty(t *testing.T) {
	dur := ParseRetryAfter("")
	if dur != 0 {
		t.Fatalf("expected 0, got %v", dur)
	}
}

func TestParseRetryAfterInvalid(t *testing.T) {
	dur := ParseRetryAfter("not-a-date")
	if dur != 0 {
		t.Fatalf("expected 0 for invalid, got %v", dur)
	}
}

func TestEventFromResponse(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "60")
	event := EventFromResponse("example.com", 429, 500*time.Millisecond, headers, false, false, time.Now())
	if event.Host != "example.com" {
		t.Fatalf("expected host, got %s", event.Host)
	}
	if event.StatusCode != 429 {
		t.Fatalf("expected 429, got %d", event.StatusCode)
	}
	if event.RetryAfter != 60*time.Second {
		t.Fatalf("expected 60s retry-after, got %v", event.RetryAfter)
	}
}

func TestEventFromResponseNoRetryAfter(t *testing.T) {
	event := EventFromResponse("example.com", 200, 100*time.Millisecond, nil, false, false, time.Now())
	if event.RetryAfter != 0 {
		t.Fatalf("expected 0 retry-after, got %v", event.RetryAfter)
	}
}

func TestImpactActionString(t *testing.T) {
	tests := []struct {
		action ImpactAction
		want   string
	}{
		{ImpactNone, "none"},
		{ImpactHalveRate, "halve_rate"},
		{ImpactQuarterRate, "quarter_rate"},
		{ImpactPause60s, "pause_60s"},
		{ImpactPause5min, "pause_5min"},
	}
	for _, tt := range tests {
		if got := tt.action.String(); got != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestImpactDetectorRateFactorDefault(t *testing.T) {
	d := NewImpactDetector()
	if d.RateFactor("unknown.com") != 1.0 {
		t.Fatal("expected default rate factor 1.0")
	}
}
