package update

import (
	"errors"

	"github.com/andyrewlee/amux/internal/logging"
)

// minisignPublicKey is the base64 minisign public key used to verify release
// signatures (the detached checksums.txt.minisig asset signed at release
// time). The operator fills in the real value when the signing keypair is
// provisioned; see the release runbook. It must always match the key whose
// secret half CI uses to sign releases, and the MINISIGN_PUBKEY value in
// install.sh.
//
// A placeholder empty value disables signature verification ONLY for dev
// builds via verifyReleaseSignature below — release (non-dev) builds fail
// closed rather than silently skipping verification.
var minisignPublicKey = ""

// verifyReleaseSignature enforces the release-signing policy for a build with
// the given version string: with an embedded public key it verifies sig over
// message; without one it skips verification only for dev builds (mirroring
// the dev-build skip in Check) and hard-fails for release builds.
func verifyReleaseSignature(version string, message, sig []byte) error {
	if minisignPublicKey == "" {
		if IsDevBuild(version) {
			logging.Debug("Skipping release signature verification: no public key embedded in dev build")
			return nil
		}
		return errors.New("no release signing public key embedded in this build; refusing to trust checksums")
	}
	return VerifyMinisign(message, sig, minisignPublicKey)
}
