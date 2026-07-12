package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// minisign wire formats (https://jedisct1.github.io/minisign/):
//
//	public key: base64(alg[2] || key_id[8] || ed25519_pub[32])
//	signature file: an "untrusted comment:" line, then
//	                base64(alg[2] || key_id[8] || ed25519_sig[64]),
//	                optionally followed by trusted-comment lines (ignored).
//
// Only the legacy "Ed" algorithm (signature over the raw message bytes) is
// supported. Release signing pins this format with `minisign -S -l` (see the
// signs block in .goreleaser.yml and the release runbook). The prehashed "ED"
// form would require BLAKE2b-512, which is deliberately not implemented so the
// updater stays on the standard library alone.

const (
	minisignAlgLegacy    = "Ed"
	minisignAlgPrehashed = "ED"

	minisignPubKeyLen = 2 + 8 + ed25519.PublicKeySize // alg + key id + key
	minisignSigLen    = 2 + 8 + ed25519.SignatureSize // alg + key id + signature
)

// VerifyMinisign verifies that sig is a valid minisign signature over message,
// made by the key whose base64 public key is pubKeyB64. It returns nil on a
// valid signature and a non-nil error otherwise (including malformed inputs).
func VerifyMinisign(message, sig []byte, pubKeyB64 string) error {
	keyID, pubKey, err := parseMinisignPublicKey(pubKeyB64)
	if err != nil {
		return fmt.Errorf("parsing minisign public key: %w", err)
	}

	alg, sigKeyID, rawSig, err := parseMinisignSignature(sig)
	if err != nil {
		return fmt.Errorf("parsing minisign signature: %w", err)
	}

	switch alg {
	case minisignAlgLegacy:
		// Signature is over the raw message bytes.
	case minisignAlgPrehashed:
		return errors.New("prehashed (ED) minisign signatures are not supported; releases must sign with the legacy format (minisign -S -l)")
	default:
		return fmt.Errorf("unsupported signature algorithm %q", alg)
	}

	// A key-id mismatch means the signature was made by a different key (for
	// example after a key rotation); reject before the cryptographic check so
	// rotation is detectable.
	if !bytes.Equal(keyID, sigKeyID) {
		return fmt.Errorf("signature key id %x does not match public key id %x", sigKeyID, keyID)
	}

	if !ed25519.Verify(ed25519.PublicKey(pubKey), message, rawSig) {
		return errors.New("invalid signature")
	}
	return nil
}

// parseMinisignPublicKey decodes a base64 minisign public key into its 8-byte
// key id and 32-byte Ed25519 public key.
func parseMinisignPublicKey(pubKeyB64 string) (keyID, pubKey []byte, err error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKeyB64))
	if err != nil {
		return nil, nil, fmt.Errorf("decoding base64: %w", err)
	}
	if len(raw) != minisignPubKeyLen {
		return nil, nil, fmt.Errorf("unexpected length %d, want %d", len(raw), minisignPubKeyLen)
	}
	if string(raw[:2]) != minisignAlgLegacy {
		return nil, nil, fmt.Errorf("unsupported key algorithm %q, want %q", raw[:2], minisignAlgLegacy)
	}
	return raw[2:10], raw[10:], nil
}

// parseMinisignSignature decodes a minisign signature file into its algorithm,
// 8-byte key id, and 64-byte Ed25519 signature. Lines after the signature line
// (trusted comment and its global signature) are ignored.
func parseMinisignSignature(sig []byte) (alg string, keyID, rawSig []byte, err error) {
	normalized := strings.ReplaceAll(string(sig), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) < 2 {
		return "", nil, nil, errors.New("signature must have a comment line and a base64 line")
	}
	if !strings.HasPrefix(lines[0], "untrusted comment:") {
		return "", nil, nil, errors.New("first line is not an untrusted comment")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return "", nil, nil, fmt.Errorf("decoding base64: %w", err)
	}
	if len(raw) != minisignSigLen {
		return "", nil, nil, fmt.Errorf("unexpected length %d, want %d", len(raw), minisignSigLen)
	}
	return string(raw[:2]), raw[2:10], raw[10:], nil
}
