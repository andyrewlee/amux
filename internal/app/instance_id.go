package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

const instanceIDPartBytes = 8

// newInstanceID combines a stable namespace for the amux state directory with
// a random per-process suffix. Sessions can therefore be reconciled across app
// restarts without crossing into a separate HOME/profile that happens to share
// the same tmux server.
func newInstanceID(stateRoot string) string {
	namespaceHash := sha256.Sum256([]byte(data.NormalizePath(strings.TrimSpace(stateRoot))))
	namespace := hex.EncodeToString(namespaceHash[:instanceIDPartBytes])

	buf := make([]byte, instanceIDPartBytes)
	if _, err := rand.Read(buf); err == nil {
		return namespace + "." + hex.EncodeToString(buf)
	}
	// Fallback to time-based value if crypto/rand fails.
	return namespace + "." + fmt.Sprintf("%016x", uint64(time.Now().UnixNano()))
}

func instanceStateNamespace(instanceID string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(instanceID), ".")
	if len(parts) != 2 || len(parts[0]) != instanceIDPartBytes*2 || len(parts[1]) != instanceIDPartBytes*2 {
		return "", false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", false
	}
	return parts[0], true
}

func instancesShareState(left, right string) bool {
	leftNamespace, leftOK := instanceStateNamespace(left)
	rightNamespace, rightOK := instanceStateNamespace(right)
	if leftOK && rightOK {
		return leftNamespace == rightNamespace
	}
	// Legacy process-only IDs had no state namespace. Only recognize the exact
	// formats older amux releases emitted; an arbitrary malformed owner tag must
	// fail closed instead of becoming eligible for cross-profile cleanup.
	if leftOK {
		return isLegacyInstanceID(right)
	}
	if rightOK {
		return isLegacyInstanceID(left)
	}
	return isLegacyInstanceID(left) && isLegacyInstanceID(right)
}

func isLegacyInstanceID(instanceID string) bool {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		// Sessions created before instance tagging are also migration candidates.
		return true
	}
	if len(id) == instanceIDPartBytes*2 {
		if _, err := hex.DecodeString(id); err == nil {
			return true
		}
	}
	// crypto/rand failure used UnixNano formatted as a positive base-10 int64.
	value, err := strconv.ParseInt(id, 10, 64)
	return err == nil && value > 0
}
