package scraper

import "sync"

// PageCache stores renderless page payloads keyed by URL.
type PageCache struct {
	mu    sync.RWMutex
	pages map[string]string
}

// NewPageCache creates an empty page cache.
func NewPageCache() *PageCache {
	return &PageCache{pages: make(map[string]string)}
}

// Get returns cached HTML for a URL.
func (c *PageCache) Get(url string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.pages[url]
	return v, ok
}

// Put stores HTML for a URL.
func (c *PageCache) Put(url string, html string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pages[url] = html
}

// Invalidate clears one URL or the full cache when url is empty.
func (c *PageCache) Invalidate(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if url == "" {
		c.pages = make(map[string]string)
		return
	}
	delete(c.pages, url)
}

// InvalidateOnNavigation drops the previous URL entry when navigating away.
func (c *PageCache) InvalidateOnNavigation(fromURL string, toURL string) {
	if fromURL == "" || fromURL == toURL {
		return
	}
	c.Invalidate(fromURL)
}
