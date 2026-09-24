package common

// FoldFingerprint mixes v into h with an FNV-1a-style step. Pane version
// fingerprints use it to fold state a mutation counter cannot observe —
// actor-mutated tab flags, time-windowed activity, terminal scroll positions —
// into a single compose-gate key (see ActivityVersion and the paneGate
// mechanism in internal/app).
func FoldFingerprint(h, v uint64) uint64 {
	return (h ^ v) * 1099511628211
}

// FoldFingerprintBool folds a boolean input into h.
func FoldFingerprintBool(h uint64, b bool) uint64 {
	if b {
		return FoldFingerprint(h, 1)
	}
	return FoldFingerprint(h, 2)
}

// FoldFingerprintString folds a string's bytes into h.
func FoldFingerprintString(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h = FoldFingerprint(h, uint64(s[i]))
	}
	return FoldFingerprint(h, uint64(len(s)))
}
