// Package update implements self-update: it compares the running version
// against GitHub releases and downloads, checksum-verifies, and installs a
// newer binary, skipping Homebrew and go-install builds.
package update
