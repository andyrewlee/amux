package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	secret := "secret"
	payload := []byte("hello")
	// Precomputed HMAC-SHA256 for secret+hello
	sig := "5d41402abc4b2a76b9719d911017c592" // wrong on purpose
	if VerifySignature(secret, payload, sig) {
		t.Fatalf("expected signature to fail")
	}

	// Compute a valid signature
	valid := computeSig(secret, payload)
	if !VerifySignature(secret, payload, valid) {
		t.Fatalf("expected signature to pass")
	}
}

func computeSig(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
