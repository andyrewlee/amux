package update

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestInstallScriptParity pins the contract install.sh shares with the release
// pipeline: the minisign pubkey and the release asset names. install.sh's
// MINISIGN_PUBKEY duplicates pubkey.go's value and the asset names it fetches
// must match .goreleaser.yml's output — nothing else verifies that, so a
// one-sided key rotation or asset rename silently breaks fresh installs while
// self-update keeps working. A failure here means rotate or rename the OTHER
// side too.
func TestInstallScriptParity(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	installSh, err := os.ReadFile(filepath.Join(repoRoot, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	goreleaser, err := os.ReadFile(filepath.Join(repoRoot, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yml: %v", err)
	}
	install := string(installSh)
	release := string(goreleaser)

	// The signing pubkey must be byte-identical on both sides.
	keyRe := regexp.MustCompile(`MINISIGN_PUBKEY="([^"]+)"`)
	m := keyRe.FindStringSubmatch(install)
	if m == nil {
		t.Fatal("install.sh defines no MINISIGN_PUBKEY — the release signing contract moved; update this test to the new shape")
	}
	if m[1] != minisignPublicKey {
		t.Fatalf("install.sh MINISIGN_PUBKEY does not match internal/update minisignPublicKey — key rotation must update both sides, or fresh installs will fail signature verification")
	}

	// The release asset names install.sh fetches must match what goreleaser
	// produces. These are literal contract members, not YAML parsing.
	for _, contract := range []struct {
		name        string
		installNeed string // substring install.sh must contain
		releaseNeed string // substring .goreleaser.yml must contain
	}{
		{"checksums file", `checksums.txt`, `name_template: "checksums.txt"`},
		{"checksums signature", `checksums.txt.minisig`, `checksums.txt`},
		{"binary+repo", `BINARY="amux"`, "andyrewlee"},
		{"release URL base", `releases/download/${VERSION}`, ""},
		{"tarball suffix", `.tar.gz`, "format: tar.gz"},
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(contract.installNeed)).MatchString(install) {
			t.Errorf("install.sh is missing contract member %q (%s) — fresh installs will 404 or verify against the wrong asset", contract.installNeed, contract.name)
		}
		if contract.releaseNeed != "" && !regexp.MustCompile(regexp.QuoteMeta(contract.releaseNeed)).MatchString(release) {
			t.Errorf(".goreleaser.yml is missing contract member %q (%s) — the release no longer produces what install.sh fetches", contract.releaseNeed, contract.name)
		}
	}

	// The archive name template must produce what FILENAME builds:
	// amux_<version>_<os>_<arch> — assert the shared shape exists on both sides.
	if !regexp.MustCompile(`name_template: "\{\{ \.ProjectName \}\}_\{\{ \.Version \}\}_\{\{ \.Os \}\}_\{\{ \.Arch \}\}"`).MatchString(release) {
		t.Error(".goreleaser.yml archive name_template changed — install.sh builds FILENAME as <binary>_<version>_<os>_<arch>.tar.gz; update both sides")
	}
	if !regexp.MustCompile(`\$\{BINARY\}_\$\{VERSION_NUM\}_\$\{OS\}_\$\{ARCH\}`).MatchString(install) {
		t.Error("install.sh FILENAME template changed — it must stay in sync with .goreleaser.yml's archive name_template")
	}
}
