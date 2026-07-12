package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testKeyID is an arbitrary fixed 8-byte minisign key id used by fixtures.
var testKeyID = []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}

// makeMinisignKeypair returns a minisign-format base64 public key carrying
// keyID and the matching Ed25519 private key.
func makeMinisignKeypair(t *testing.T, keyID []byte) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating keypair: %v", err)
	}
	raw := append([]byte(minisignAlgLegacy), keyID...)
	raw = append(raw, pub...)
	return base64.StdEncoding.EncodeToString(raw), priv
}

// encodeMinisignSig assembles a minisign signature file from raw parts,
// including the trusted-comment lines a real minisign CLI emits (the parser
// must tolerate them).
func encodeMinisignSig(alg string, keyID, rawSig []byte) []byte {
	raw := append([]byte(alg), keyID...)
	raw = append(raw, rawSig...)
	var b bytes.Buffer
	b.WriteString("untrusted comment: signature from amux test key\n")
	b.WriteString(base64.StdEncoding.EncodeToString(raw))
	b.WriteString("\ntrusted comment: timestamp:0\n")
	b.WriteString(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))
	b.WriteString("\n")
	return b.Bytes()
}

// makeMinisignSig signs message with priv and wraps it in the minisign
// signature-file format.
func makeMinisignSig(t *testing.T, priv ed25519.PrivateKey, alg string, keyID, message []byte) []byte {
	t.Helper()
	return encodeMinisignSig(alg, keyID, ed25519.Sign(priv, message))
}

func TestVerifyMinisignValid(t *testing.T) {
	message := []byte("deadbeef  amux_1.0.0_darwin_arm64.tar.gz\n")
	pub, priv := makeMinisignKeypair(t, testKeyID)
	sig := makeMinisignSig(t, priv, minisignAlgLegacy, testKeyID, message)

	if err := VerifyMinisign(message, sig, pub); err != nil {
		t.Fatalf("VerifyMinisign() error = %v, want nil", err)
	}
}

func TestVerifyMinisignFailures(t *testing.T) {
	message := []byte("deadbeef  amux_1.0.0_darwin_arm64.tar.gz\n")
	pub, priv := makeMinisignKeypair(t, testKeyID)
	validSig := makeMinisignSig(t, priv, minisignAlgLegacy, testKeyID, message)

	otherKeyID := []byte{0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10}
	otherPub, otherPriv := makeMinisignKeypair(t, otherKeyID)

	tamperedSigBytes := ed25519.Sign(priv, message)
	tamperedSigBytes[0] ^= 0x01

	truncated := append([]byte(minisignAlgLegacy), testKeyID...)
	truncated = append(truncated, ed25519.Sign(priv, message)[:ed25519.SignatureSize-1]...)
	var truncatedFile bytes.Buffer
	truncatedFile.WriteString("untrusted comment: truncated\n")
	truncatedFile.WriteString(base64.StdEncoding.EncodeToString(truncated))
	truncatedFile.WriteString("\n")

	tests := []struct {
		name    string
		message []byte
		sig     []byte
		pub     string
	}{
		{
			name:    "wrong key",
			message: message,
			sig:     validSig,
			pub:     otherPub,
		},
		{
			name:    "tampered message",
			message: []byte("attacker-controlled checksums\n"),
			sig:     validSig,
			pub:     pub,
		},
		{
			name:    "tampered signature bytes",
			message: message,
			sig:     encodeMinisignSig(minisignAlgLegacy, testKeyID, tamperedSigBytes),
			pub:     pub,
		},
		{
			name:    "mismatched key id",
			message: message,
			sig:     makeMinisignSig(t, otherPriv, minisignAlgLegacy, otherKeyID, message),
			pub:     pub,
		},
		{
			name:    "prehashed ED algorithm rejected",
			message: message,
			sig:     makeMinisignSig(t, priv, minisignAlgPrehashed, testKeyID, message),
			pub:     pub,
		},
		{
			name:    "malformed base64 signature",
			message: message,
			sig:     []byte("untrusted comment: bad\n!!!not-base64!!!\n"),
			pub:     pub,
		},
		{
			name:    "truncated signature",
			message: message,
			sig:     truncatedFile.Bytes(),
			pub:     pub,
		},
		{
			name:    "missing comment line",
			message: message,
			sig:     bytes.SplitN(validSig, []byte("\n"), 2)[1],
			pub:     pub,
		},
		{
			name:    "empty signature",
			message: message,
			sig:     nil,
			pub:     pub,
		},
		{
			name:    "malformed base64 public key",
			message: message,
			sig:     validSig,
			pub:     "!!!not-base64!!!",
		},
		{
			name:    "public key wrong length",
			message: message,
			sig:     validSig,
			pub:     base64.StdEncoding.EncodeToString([]byte("Ed-too-short")),
		},
		{
			name:    "empty public key",
			message: message,
			sig:     validSig,
			pub:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifyMinisign(tt.message, tt.sig, tt.pub); err == nil {
				t.Fatal("VerifyMinisign() = nil, want error")
			}
		})
	}
}

// TestVerifyReleaseSignaturePolicy pins the fail-closed policy: an empty
// embedded public key may skip verification only for dev builds; release
// version strings must hard-fail rather than silently skip.
func TestVerifyReleaseSignaturePolicy(t *testing.T) {
	original := minisignPublicKey
	t.Cleanup(func() { minisignPublicKey = original })

	message := []byte("deadbeef  amux_1.0.0_darwin_arm64.tar.gz\n")
	pub, priv := makeMinisignKeypair(t, testKeyID)
	sig := makeMinisignSig(t, priv, minisignAlgLegacy, testKeyID, message)

	minisignPublicKey = ""
	if err := verifyReleaseSignature("dev", message, sig); err != nil {
		t.Errorf("empty key + dev build: error = %v, want skip (nil)", err)
	}
	if err := verifyReleaseSignature("v1.2.3", message, sig); err == nil {
		t.Error("empty key + release build: error = nil, want fail-closed error")
	}

	minisignPublicKey = pub
	if err := verifyReleaseSignature("v1.2.3", message, sig); err != nil {
		t.Errorf("embedded key + valid signature: error = %v, want nil", err)
	}
	if err := verifyReleaseSignature("v1.2.3", []byte("tampered"), sig); err == nil {
		t.Error("embedded key + tampered message: error = nil, want error")
	}
	// With a key embedded, even dev builds must verify (the skip applies only
	// to the missing-key case).
	if err := verifyReleaseSignature("dev", []byte("tampered"), sig); err == nil {
		t.Error("embedded key + dev build + tampered message: error = nil, want error")
	}
}

// TestUpdaterVerifySignatureClosure exercises the exact closure NewUpdater
// wires into upgradeDeps, end to end against a fixture checksums.txt.
func TestUpdaterVerifySignatureClosure(t *testing.T) {
	original := minisignPublicKey
	t.Cleanup(func() { minisignPublicKey = original })

	fixture := []byte("0123456789abcdef  amux_1.0.0_darwin_arm64.tar.gz\nfedcba9876543210  amux_1.0.0_linux_amd64.tar.gz\n")
	pub, priv := makeMinisignKeypair(t, testKeyID)
	sig := makeMinisignSig(t, priv, minisignAlgLegacy, testKeyID, fixture)
	minisignPublicKey = pub

	u := NewUpdater("v1.0.0", "none", "unknown")
	if err := u.deps.verifySignature(fixture, sig); err != nil {
		t.Fatalf("verifySignature(valid) error = %v, want nil", err)
	}

	tampered := append([]byte(nil), fixture...)
	tampered[0] ^= 0x01
	if err := u.deps.verifySignature(tampered, sig); err == nil {
		t.Fatal("verifySignature(tampered) = nil, want error")
	}
}

// TestVerifyMinisignCLIParity signs a fixture with the real minisign CLI using
// the exact argument shape from the GoReleaser signs block and asserts the Go
// verifier accepts the output. Skipped when minisign is not installed.
func TestVerifyMinisignCLIParity(t *testing.T) {
	minisignPath, err := exec.LookPath("minisign")
	if err != nil {
		t.Skip("minisign CLI not installed; skipping CLI-parity test")
	}

	dir := t.TempDir()
	secPath := filepath.Join(dir, "minisign.key")
	pubPath := filepath.Join(dir, "minisign.pub")

	// -W: unencrypted secret key so the test runs non-interactively.
	if out, err := exec.Command(minisignPath, "-G", "-f", "-W", "-p", pubPath, "-s", secPath).CombinedOutput(); err != nil {
		t.Fatalf("minisign -G failed: %v\n%s", err, out)
	}

	fixture := []byte("deadbeef  amux_1.0.0_darwin_arm64.tar.gz\n")
	msgPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(msgPath, fixture, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	sigPath := msgPath + ".minisig"

	// Exact argument shape used by the signs block in .goreleaser.yml; -l pins
	// the legacy (non-prehashed) format the Go verifier implements.
	if out, err := exec.Command(minisignPath, "-S", "-l", "-s", secPath, "-m", msgPath, "-x", sigPath).CombinedOutput(); err != nil {
		t.Fatalf("minisign -S -l failed: %v\n%s", err, out)
	}

	pubFile, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("reading public key file: %v", err)
	}
	pubLines := strings.Split(strings.TrimSpace(string(pubFile)), "\n")
	if len(pubLines) < 2 {
		t.Fatalf("unexpected public key file format:\n%s", pubFile)
	}
	pubB64 := strings.TrimSpace(pubLines[len(pubLines)-1])

	sig, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatalf("reading signature file: %v", err)
	}

	if err := VerifyMinisign(fixture, sig, pubB64); err != nil {
		t.Fatalf("VerifyMinisign rejected real minisign CLI output: %v", err)
	}

	tampered := append([]byte(nil), fixture...)
	tampered[0] ^= 0x01
	if err := VerifyMinisign(tampered, sig, pubB64); err == nil {
		t.Fatal("VerifyMinisign accepted CLI signature over tampered message")
	}
}
