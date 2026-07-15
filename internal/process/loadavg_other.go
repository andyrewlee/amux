//go:build !darwin && !linux

package process

import "errors"

func loadAverage1m() (float64, error) {
	return 0, errors.ErrUnsupported
}
