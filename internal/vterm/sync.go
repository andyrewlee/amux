package vterm

// SyncActive reports whether synchronized output is currently active.
func (v *VTerm) SyncActive() bool {
	return v.syncActive
}

func (v *VTerm) setSynchronizedOutput(active bool) {
	if active {
		if v.syncActive {
			return
		}
		v.syncActive = true
		v.syncScreen = copyScreen(v.Screen)
		v.syncScrollbackLen = len(v.Scrollback)
		v.invalidateRenderCache()
		return
	}

	if !v.syncActive {
		return
	}
	v.syncActive = false
	v.syncScreen = nil
	v.syncScrollbackLen = 0
	if v.syncDeferTrim {
		v.syncDeferTrim = false
		v.trimScrollback()
	}
	v.invalidateRenderCache()
}

func copyScreen(src [][]Cell) [][]Cell {
	dst := make([][]Cell, len(src))
	for i := range src {
		dst[i] = CopyLine(src[i])
	}
	return dst
}
