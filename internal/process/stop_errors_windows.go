//go:build windows

package process

func isTypedProcessGoneError(err error) bool {
	_ = err
	return false
}
