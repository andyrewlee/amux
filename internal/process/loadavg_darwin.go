//go:build darwin

package process

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// loadAverage1m reads the 1-minute load average from vm.loadavg. The sysctl
// returns struct loadavg { fixpt_t ldavg[3]; long fscale; }: three uint32
// fixed-point samples, 4 bytes of alignment padding, then an int64 scale.
func loadAverage1m() (float64, error) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0, err
	}
	if len(raw) < 24 {
		return 0, fmt.Errorf("vm.loadavg: unexpected %d-byte payload", len(raw))
	}
	ldavg := binary.LittleEndian.Uint32(raw[0:4])
	fscale := int64(binary.LittleEndian.Uint64(raw[16:24]))
	if fscale <= 0 {
		return 0, fmt.Errorf("vm.loadavg: invalid fscale %d", fscale)
	}
	return float64(ldavg) / float64(fscale), nil
}
