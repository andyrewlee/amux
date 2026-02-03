package update

import "strings"

// homebrewBuild is set via ldflags in the Homebrew formula.
// Example: -X github.com/andyrewlee/amux/internal/update.homebrewBuild=true
var homebrewBuild = "false"

// IsHomebrewBuild returns true when the binary was built for Homebrew.
func IsHomebrewBuild() bool {
	raw := strings.TrimSpace(strings.ToLower(homebrewBuild))
	return raw == "1" || raw == "true" || raw == "yes"
}
