package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestPrefixPaletteGeometry_ReusesComposeAndInvalidatesOnResize pins the
// cache contract: once a compose has measured the palette, the
// hit-test consumes that measurement instead of re-rendering; a size change
// (or no compose yet) falls back to measuring live.
func TestPrefixPaletteGeometry_ReusesComposeAndInvalidatesOnResize(t *testing.T) {
	app := &App{prefixActive: true, width: 120, height: 40}

	_, liveH := viewDimensions(app.renderPrefixPalette())
	if liveH <= 0 {
		t.Fatal("expected the palette to render a non-zero height")
	}

	// A composed snapshot whose palette height deliberately differs from the
	// live render — if the hit-test consumed the cache, its boundary follows
	// the sentinel exactly.
	sentinel := liveH + 4
	app.overlayGeom = overlayGeometry{composed: true, width: 120, height: 40, paletteH: sentinel}
	if got := app.prefixPaletteHeight(); got != sentinel {
		t.Fatalf("prefixPaletteHeight() = %d, want composed %d", got, sentinel)
	}
	if !app.prefixPaletteContainsPoint(10, 40-sentinel) {
		t.Fatal("hit-test should follow the composed palette top edge")
	}
	if app.prefixPaletteContainsPoint(10, 40-sentinel-1) {
		t.Fatal("hit-test should not extend above the composed palette")
	}

	// A resize invalidates the snapshot — live measurement resumes.
	app.width = 200
	_, resizedH := viewDimensions(app.renderPrefixPalette())
	if got := app.prefixPaletteHeight(); got != resizedH {
		t.Fatalf("after resize prefixPaletteHeight() = %d, want live %d", got, resizedH)
	}
}

// TestToastCoversPoint_UsesComposedGeometry pins the same contract for the
// toast rect: composed dims drive the cover test, and a stale snapshot falls
// back to the live view.
func TestToastCoversPoint_UsesComposedGeometry(t *testing.T) {
	app := &App{width: 120, height: 40, toast: common.NewToastModel()}
	_ = app.toast.ShowInfo("hello")

	toastView := app.toast.View()
	liveW, liveH := viewDimensions(toastView)

	// Sentinel toast rect: 11x3 — coverage must follow it, not the live view.
	app.overlayGeom = overlayGeometry{
		composed: true, width: 120, height: 40,
		toastW: 11, toastH: 3,
	}
	tx := (120 - 11) / 2
	ty := 40 - 2
	if !app.toastCoversPoint(tx, ty) || !app.toastCoversPoint(tx+10, ty+2) {
		t.Fatal("expected points inside the composed toast rect to be covered")
	}
	if app.toastCoversPoint(tx-1, ty) || app.toastCoversPoint(tx, ty-1) {
		t.Fatal("expected points outside the composed toast rect to miss")
	}
	_ = liveW
	_ = liveH

	// Stale snapshot (wrong size) → live view measured again.
	app.overlayGeom.width = 999
	liveX := (120 - liveW) / 2
	liveY := 40 - 2
	if !app.toastCoversPoint(liveX, liveY) {
		t.Fatal("expected the live toast rect to be covered after invalidation")
	}
}
