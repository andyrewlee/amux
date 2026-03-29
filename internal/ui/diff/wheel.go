package diff

// CanConsumeWheel reports whether the diff viewer has enough content for
// mouse-wheel scrolling to have an effect.
func (m *Model) CanConsumeWheel() bool {
	if m == nil {
		return false
	}
	return m.maxScroll() > 0
}
