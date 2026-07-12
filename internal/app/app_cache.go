package app

import (
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

type drawableCache struct {
	content  string
	x, y     int
	drawable *compositor.StringDrawable
}

func (c *drawableCache) get(content string, x, y int) *compositor.StringDrawable {
	if content == "" {
		c.content = ""
		c.drawable = nil
		return nil
	}
	if c.drawable != nil && c.content == content && c.x == x && c.y == y {
		return c.drawable
	}
	c.content = content
	c.x = x
	c.y = y
	c.drawable = compositor.NewStringDrawable(content, x, y)
	return c.drawable
}

type borderCache struct {
	x, y      int
	width     int
	height    int
	themeID   common.ThemeID
	drawables []*compositor.StringDrawable
}

func (c *borderCache) get(x, y, width, height int) []*compositor.StringDrawable {
	themeID := common.GetCurrentTheme().ID
	if c.drawables != nil &&
		c.x == x && c.y == y &&
		c.width == width && c.height == height &&
		c.themeID == themeID {
		return c.drawables
	}
	c.x = x
	c.y = y
	c.width = width
	c.height = height
	c.themeID = themeID
	c.drawables = borderDrawables(x, y, width, height)
	return c.drawables
}

// paneGate records the pane-reported content version and compose geometry
// that produced the current cached pane drawable, letting the compose path
// skip a clean pane's string build entirely (an earlier skip than the
// content-keyed drawableCache, which still has to rebuild the string to key
// on it). rendered remembers whether that build composed a drawable so a
// clean skip reproduces the same layout decision (e.g. tab bar height)
// without rebuilding. Correctness rests on the pane bumping its version on
// every content-affecting update; see the version-field invariants on the
// pane structs.
type paneGate struct {
	valid    bool
	version  uint64
	geom     [4]int
	rendered bool
}

// clean reports whether the pane's string build can be skipped: the gate has
// recorded a build for the same pane version at the same compose geometry.
func (g *paneGate) clean(version uint64, geom [4]int) bool {
	return g.valid && g.version == version && g.geom == geom
}

// record notes the version/geometry that produced the current cache entry and
// whether that build composed a drawable.
func (g *paneGate) record(version uint64, geom [4]int, rendered bool) {
	g.valid = true
	g.version = version
	g.geom = geom
	g.rendered = rendered
}

// renderCacheState groups the chrome/drawable caches used by layer-based
// rendering. Each cache is keyed on the inputs that produced it and reused
// across frames until those inputs change.
type renderCacheState struct {
	dashboardChrome      *compositor.ChromeCache
	centerChrome         *compositor.ChromeCache
	sidebarChrome        *compositor.ChromeCache
	dashboardContent     drawableCache
	dashboardBorders     borderCache
	sidebarTopTabBar     drawableCache
	sidebarTopTabBarGate paneGate
	sidebarTopContent    drawableCache
	sidebarBottomContent drawableCache
	sidebarBottomTabBar  drawableCache
	sidebarBottomStatus  drawableCache
	sidebarBottomHelp    drawableCache
	sidebarTopBorders    borderCache
	sidebarBottomBorders borderCache
	centerTabBar         drawableCache
	centerStatus         drawableCache
	centerHelp           drawableCache
	centerHelpGate       paneGate
	centerBorders        borderCache
}

func newRenderCacheState() renderCacheState {
	return renderCacheState{
		dashboardChrome: &compositor.ChromeCache{},
		centerChrome:    &compositor.ChromeCache{},
		sidebarChrome:   &compositor.ChromeCache{},
	}
}
