//go:build linux

package process

import "golang.org/x/sys/unix"

// loadAverage1m reads the 1-minute load average via sysinfo(2), whose Loads
// values are fixed-point with a 16-bit fractional part (SI_LOAD_SHIFT).
func loadAverage1m() (float64, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, err
	}
	return float64(info.Loads[0]) / float64(1<<16), nil
}
