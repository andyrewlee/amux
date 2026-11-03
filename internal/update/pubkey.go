package update

import (
	"errors"

	"github.com/andyrewlee/amux/internal/logging"
)

// minisignPublicKey is the base64 minisign public key used to verify release
// signatures (the detached checksums.txt.minisig asset signed at release
// time). The operator fills in the real value when the signing keypair is
// provisioned; see the release runbook. It must always match the minisign
// secret key used by `goreleaser release --clean` in the release operator's
// local environment, and the MINISIGN_PUBKEY value in install.sh.
//
// Rotated 2026-11-03: the original keypair (provisioned 2026-07-16) was
// lost, so binaries built before this rotation cannot verify releases
// signed by the new key — their in-app updater refuses the checksums, and
// installing such a release requires AMUX_ALLOW_UNVERIFIED=1 (install.sh)
// or a manual download. Fresh installs pin this key.
//
// A placeholder empty value disables signature verification ONLY for dev
// builds via verifyReleaseSignature below — release (non-dev) builds fail
// closed rather than silently skipping verification.
var minisignPublicKey = "RWRo9OMQinVnxL1KMY2M3oepJ+iEVnQLy/Dy8RNkW77/BQ7F6oBntn5F"

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
