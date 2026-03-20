package harnessbench

import "time"

func sortDurations(values []time.Duration) {
	for i := 1; i < len(values); i++ {
		current := values[i]
		j := i - 1
		for ; j >= 0 && values[j] > current; j-- {
			values[j+1] = values[j]
		}
		values[j+1] = current
	}
}
