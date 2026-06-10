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
	centerBorders        borderCache
}

func newRenderCacheState() renderCacheState {
	return renderCacheState{
		dashboardChrome: &compositor.ChromeCache{},
		centerChrome:    &compositor.ChromeCache{},
		sidebarChrome:   &compositor.ChromeCache{},
	}
}
