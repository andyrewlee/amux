package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// VerifySignature verifies the Linear webhook signature against the raw payload.
func VerifySignature(secret string, payload []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	given, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, given)
}
