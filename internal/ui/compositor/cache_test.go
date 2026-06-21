package compositor

import (
	"testing"
)

// validParams returns a representative set of cache parameters used to prime
// the cache for most table-driven hit/miss tests.
func validParams() (content string, width, height int, focused bool, posX, posY int) {
	return "│ pane │", 80, 24, true, 3, 5
}

func TestChromeCacheGetEmptyCacheReturnsNil(t *testing.T) {
	c := &ChromeCache{}
	content, width, height, focused, posX, posY := validParams()
	if got := c.Get(content, width, height, focused, posX, posY); got != nil {
		t.Fatalf("expected nil from empty cache, got %v", got)
	}
}

func TestChromeCacheGetHitReturnsSameDrawable(t *testing.T) {
	c := &ChromeCache{}
	content, width, height, focused, posX, posY := validParams()
	want := NewStringDrawable(content, posX, posY)
	c.Set(content, width, height, focused, posX, posY, want)

	got := c.Get(content, width, height, focused, posX, posY)
	if got == nil {
		t.Fatalf("expected cache hit, got nil")
	}
	if got != want {
		t.Fatalf("expected identical drawable pointer on hit, got %p want %p", got, want)
	}
}

// TestChromeCacheGetMissPerField asserts that changing any single field of the
// lookup key invalidates the cache hit, exercising every branch of Get's guard.
func TestChromeCacheGetMissPerField(t *testing.T) {
	content, width, height, focused, posX, posY := validParams()

	tests := []struct {
		name    string
		content string
		width   int
		height  int
		focused bool
		posX    int
		posY    int
	}{
		{"content differs", content + "x", width, height, focused, posX, posY},
		{"width differs", content, width + 1, height, focused, posX, posY},
		{"height differs", content, width, height + 1, focused, posX, posY},
		{"focused differs", content, width, height, !focused, posX, posY},
		{"posX differs", content, width, height, focused, posX + 1, posY},
		{"posY differs", content, width, height, focused, posX, posY + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChromeCache{}
			c.Set(content, width, height, focused, posX, posY, NewStringDrawable(content, posX, posY))
			if got := c.Get(tt.content, tt.width, tt.height, tt.focused, tt.posX, tt.posY); got != nil {
				t.Fatalf("expected cache miss when %s, got non-nil drawable", tt.name)
			}
		})
	}
}

// TestChromeCacheGetNilDrawableNeverHits ensures a key match still returns nil
// when no drawable was stored (e.g. fields set but drawable left nil).
func TestChromeCacheGetNilDrawableNeverHits(t *testing.T) {
	content, width, height, focused, posX, posY := validParams()
	c := &ChromeCache{
		content: content,
		width:   width,
		height:  height,
		focused: focused,
		posX:    posX,
		posY:    posY,
		// drawable intentionally nil
	}
	if got := c.Get(content, width, height, focused, posX, posY); got != nil {
		t.Fatalf("expected nil when drawable is nil even on key match, got %v", got)
	}
}

func TestChromeCacheSetStoresAllFields(t *testing.T) {
	c := &ChromeCache{}
	content, width, height, focused, posX, posY := "abc", 12, 34, true, 7, 9
	drawable := NewStringDrawable(content, posX, posY)

	c.Set(content, width, height, focused, posX, posY, drawable)

	if c.content != content {
		t.Errorf("content = %q, want %q", c.content, content)
	}
	if c.width != width {
		t.Errorf("width = %d, want %d", c.width, width)
	}
	if c.height != height {
		t.Errorf("height = %d, want %d", c.height, height)
	}
	if c.focused != focused {
		t.Errorf("focused = %v, want %v", c.focused, focused)
	}
	if c.posX != posX {
		t.Errorf("posX = %d, want %d", c.posX, posX)
	}
	if c.posY != posY {
		t.Errorf("posY = %d, want %d", c.posY, posY)
	}
	if c.drawable != drawable {
		t.Errorf("drawable = %p, want %p", c.drawable, drawable)
	}
}

// TestChromeCacheSetOverwrites verifies Set fully replaces a prior entry so a
// stale key no longer hits and the new key does.
func TestChromeCacheSetOverwrites(t *testing.T) {
	c := &ChromeCache{}
	first := NewStringDrawable("first", 0, 0)
	c.Set("first", 10, 10, false, 0, 0, first)

	second := NewStringDrawable("second", 1, 1)
	c.Set("second", 20, 20, true, 1, 1, second)

	if got := c.Get("first", 10, 10, false, 0, 0); got != nil {
		t.Fatalf("expected old key to miss after overwrite, got %v", got)
	}
	got := c.Get("second", 20, 20, true, 1, 1)
	if got != second {
		t.Fatalf("expected new key to hit with new drawable, got %p want %p", got, second)
	}
}

// TestChromeCacheSetEmptyContent covers the empty-string boundary: an empty
// content must still round-trip as a valid, retrievable cache entry.
func TestChromeCacheSetEmptyContent(t *testing.T) {
	c := &ChromeCache{}
	drawable := NewStringDrawable("", 0, 0)
	c.Set("", 0, 0, false, 0, 0, drawable)

	got := c.Get("", 0, 0, false, 0, 0)
	if got != drawable {
		t.Fatalf("expected empty-content entry to hit, got %p want %p", got, drawable)
	}
}

func TestChromeCacheInvalidateClearsHit(t *testing.T) {
	c := &ChromeCache{}
	content, width, height, focused, posX, posY := validParams()
	c.Set(content, width, height, focused, posX, posY, NewStringDrawable(content, posX, posY))

	if c.Get(content, width, height, focused, posX, posY) == nil {
		t.Fatalf("precondition failed: expected primed cache to hit")
	}

	c.Invalidate()

	if got := c.Get(content, width, height, focused, posX, posY); got != nil {
		t.Fatalf("expected miss after Invalidate, got %v", got)
	}
	if c.drawable != nil {
		t.Errorf("expected drawable nil after Invalidate, got %v", c.drawable)
	}
	if c.content != "" {
		t.Errorf("expected content cleared after Invalidate, got %q", c.content)
	}
}

// TestChromeCacheInvalidateIdempotent ensures Invalidate is safe on a fresh
// (already-empty) cache.
func TestChromeCacheInvalidateIdempotent(t *testing.T) {
	c := &ChromeCache{}
	c.Invalidate()
	c.Invalidate()
	if c.drawable != nil || c.content != "" {
		t.Fatalf("expected empty cache to stay empty after repeated Invalidate")
	}
}

// TestChromeCacheReuseAfterInvalidate verifies the cache can be re-primed and
// hit again after invalidation (the rebuild path used by the compositor).
func TestChromeCacheReuseAfterInvalidate(t *testing.T) {
	c := &ChromeCache{}
	content, width, height, focused, posX, posY := validParams()
	c.Set(content, width, height, focused, posX, posY, NewStringDrawable(content, posX, posY))
	c.Invalidate()

	reborn := NewStringDrawable(content, posX, posY)
	c.Set(content, width, height, focused, posX, posY, reborn)
	if got := c.Get(content, width, height, focused, posX, posY); got != reborn {
		t.Fatalf("expected re-primed cache to hit new drawable, got %p want %p", got, reborn)
	}
}
