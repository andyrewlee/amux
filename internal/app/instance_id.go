package app

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

func newInstanceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	// Fallback to time-based value if crypto/rand fails.
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
