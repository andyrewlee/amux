package compositor

// ChromeCache caches a StringDrawable for static pane chrome.
// Reuse the cached drawable when layout/content hasn't changed.
type ChromeCache struct {
	width    int
	height   int
	focused  bool
	posX     int
	posY     int
	content  string
	drawable *StringDrawable
}

// Get returns the cached StringDrawable if the parameters match.
// Returns nil if cache is invalid and needs rebuild.
func (c *ChromeCache) Get(content string, width, height int, focused bool, posX, posY int) *StringDrawable {
	if c.drawable != nil &&
		c.content == content &&
		c.width == width &&
		c.height == height &&
		c.focused == focused &&
		c.posX == posX &&
		c.posY == posY {
		return c.drawable
	}
	return nil
}

// Set updates the cache with a new StringDrawable.
func (c *ChromeCache) Set(content string, width, height int, focused bool, posX, posY int, drawable *StringDrawable) {
	c.content = content
	c.width = width
	c.height = height
	c.focused = focused
	c.posX = posX
	c.posY = posY
	c.drawable = drawable
}

// Invalidate clears the cache.
func (c *ChromeCache) Invalidate() {
	c.drawable = nil
	c.content = ""
}

// FastHash computes a FNV-1a hash for cache invalidation benchmarks.
func FastHash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
