package compositor

import (
	"testing"
)

// TestRGBColorCacheBounded: driving more than rgbCacheCap unique colors
// through rgbToUV swaps the cache epoch instead of growing without bound —
// a truecolor-dense stream can mint ~16.7M distinct 24-bit keys.
func TestRGBColorCacheBounded(t *testing.T) {
	// A color minted pre-swap still resolves after the epoch flips — it is
	// simply re-boxed into the fresh map.
	first := rgbToUV(0x112233)

	for i := 0; i < rgbCacheCap+4096; i++ {
		rgbToUV(uint32(i))
	}
	if got := rgbColorCacheLen.Load(); got >= rgbCacheCap {
		t.Fatalf("cache counter = %d after %d stores, want < cap %d", got, rgbCacheCap+4096, rgbCacheCap)
	}
	// The live epoch must be under the cap too (the counter resets at swap).
	count := 0
	rgbColorCache.Load().Range(func(_, _ any) bool {
		count++
		return true
	})
	if count > rgbCacheCap {
		t.Fatalf("live cache holds %d entries, want <= %d", count, rgbCacheCap)
	}

	if got := rgbToUV(0x112233); got != first {
		t.Fatalf("cached color must still resolve after an epoch swap: got %v want %v", got, first)
	}
}
