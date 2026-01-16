package compositor

import (
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
)

// cellPool reuses uv.Cell allocations across the compositor.
//
// This is safe because ultraviolet's Buffer.SetCell copies the Cell value
// (not the pointer), so we can return cells to the pool immediately after
// SetCell returns. See: ultraviolet/buffer.go Line.Set() does `l[x] = *c`
var cellPool = sync.Pool{
	New: func() any { return &uv.Cell{} },
}

// getCell gets a cell from the pool, resets it, and returns it ready for use.
func getCell() *uv.Cell {
	c, _ := cellPool.Get().(*uv.Cell)
	if c == nil {
		c = &uv.Cell{}
	} else {
		*c = uv.Cell{}
	}
	return c
}

// putCell returns a cell to the pool.
func putCell(c *uv.Cell) {
	cellPool.Put(c)
}
