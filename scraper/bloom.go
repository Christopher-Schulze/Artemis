package scraper

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

// URLDeduper deduplicates crawl URLs with Bloom filter for large sets (spec P7.3).
type URLDeduper struct {
	mu      sync.Mutex
	bloom   *bloom.BloomFilter
	mapMode map[string]struct{}
	limit   int
}

// NewURLDeduper creates a deduper. Uses map below limit, Bloom at or above.
func NewURLDeduper(expected uint, falsePositive float64) *URLDeduper {
	if expected < 10000 {
		return &URLDeduper{mapMode: make(map[string]struct{}), limit: 10000}
	}
	if falsePositive <= 0 {
		falsePositive = 0.001
	}
	return &URLDeduper{bloom: bloom.NewWithEstimates(expected, falsePositive), limit: 10000}
}

// Seen reports whether url was already recorded; if not, records it.
func (d *URLDeduper) Seen(url string) bool {
	if d == nil || url == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mapMode != nil {
		_, ok := d.mapMode[url]
		if ok {
			return true
		}
		d.mapMode[url] = struct{}{}
		return false
	}
	if d.bloom.Test([]byte(url)) {
		return true
	}
	d.bloom.Add([]byte(url))
	return false
}
