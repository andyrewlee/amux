package process

import "runtime"

// LoadPerCore returns the 1-minute load average divided by the CPU count —
// values well above 1 mean more runnable threads than cores. Used to detect
// the "machine is drowning" state so the UI can say so instead of surfacing
// mysterious timeouts. Unsupported platforms return an error.
func LoadPerCore() (float64, error) {
	load, err := loadAverage1m()
	if err != nil {
		return 0, err
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	return load / float64(cores), nil
}
