package observe

import (
	"fmt"
	"sync"
	"time"
)

// metrics.go (observe/metrics.go - performance metrics).
//
// Page observation: performance metrics capture and tracking.

// PerformanceMetrics captures browser performance metrics
// (metrics.go - performance metrics).
type PerformanceMetrics struct {
	DOMContentLoaded       time.Duration `json:"domContentLoaded"`       // DOMContentLoaded event time
	LoadEvent              time.Duration `json:"loadEvent"`              // load event time
	FirstPaint             time.Duration `json:"firstPaint"`             // first paint time
	FirstContentfulPaint   time.Duration `json:"firstContentfulPaint"`   // FCP
	LargestContentfulPaint time.Duration `json:"largestContentfulPaint"` // LCP
	TimeToInteractive      time.Duration `json:"timeToInteractive"`      // TTI
	TotalBlockingTime      time.Duration `json:"totalBlockingTime"`      // TBT
	CumulativeLayoutShift  float64       `json:"cumulativeLayoutShift"`  // CLS
	TransferSize           int64         `json:"transferSize"`           // total bytes transferred
	EncodedSize            int64         `json:"encodedSize"`            // encoded body size
	DecodedSize            int64         `json:"decodedSize"`            // decoded body size
	RequestCount           int           `json:"requestCount"`           // number of requests
}

// MetricsCollector collects performance metrics
// (metrics.go - performance metrics).
type MetricsCollector struct {
	mu        sync.RWMutex
	current   PerformanceMetrics
	updatedAt time.Time
}

// NewMetricsCollector creates a new MetricsCollector
// (metrics.go).
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// SetMetrics updates the current performance metrics
// (metrics.go - performance metrics).
func (c *MetricsCollector) SetMetrics(m PerformanceMetrics) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = m
	c.updatedAt = time.Now()
}

// Current returns the current performance metrics.
func (c *MetricsCollector) Current() PerformanceMetrics {
	if c == nil {
		return PerformanceMetrics{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// UpdatedAt returns when the metrics were last updated.
func (c *MetricsCollector) UpdatedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}

// IsEmpty reports whether no metrics have been collected
// (metrics.go).
func (c *MetricsCollector) IsEmpty() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt.IsZero()
}

// String returns a diagnostic summary.
func (m PerformanceMetrics) String() string {
	return fmt.Sprintf("PerformanceMetrics{DCL:%v Load:%v FCP:%v LCP:%v requests:%d transfer:%d}",
		m.DOMContentLoaded, m.LoadEvent, m.FirstContentfulPaint,
		m.LargestContentfulPaint, m.RequestCount, m.TransferSize)
}
