package common

// ScrollDeltaForHeight calculates proportional scroll delta.
// Returns max(1, height/factor) to ensure minimum 1 line scroll.
func ScrollDeltaForHeight(height, factor int) int {
	delta := height / factor
	if delta < 1 {
		delta = 1
	}
	return delta
}
