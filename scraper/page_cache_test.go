package scraper

import "testing"

func TestPageCacheInvalidation(t *testing.T) {
	c := NewPageCache()
	c.Put("https://example.com/a", "<html>a</html>")
	c.InvalidateOnNavigation("https://example.com/a", "https://example.com/b")
	if _, ok := c.Get("https://example.com/a"); ok {
		t.Fatal("expected navigation invalidation")
	}
	c.Put("https://example.com/b", "<html>b</html>")
	c.Invalidate("")
	if c.Len() != 0 {
		t.Fatal("expected full clear")
	}
}

func (c *PageCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.pages)
}
