package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a semantic version.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Raw        string
}

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([a-zA-Z0-9.-]+))?$`)

// ParseVersion parses a semantic version string.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	matches := semverRegex.FindStringSubmatch(s)
	if matches == nil {
		return Version{}, fmt.Errorf("invalid version format: %s", s)
	}

	major, err := parseVersionComponent("major", matches[1])
	if err != nil {
		return Version{}, err
	}
	minor, err := parseVersionComponent("minor", matches[2])
	if err != nil {
		return Version{}, err
	}
	patch, err := parseVersionComponent("patch", matches[3])
	if err != nil {
		return Version{}, err
	}
	prerelease := ""
	if len(matches) > 4 {
		prerelease = matches[4]
	}

	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: prerelease,
		Raw:        s,
	}, nil
}

func parseVersionComponent(name, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s version %q: %w", name, value, err)
	}
	return n, nil
}

// String returns the version string with "v" prefix.
func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	return s
}

// Compare returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	// Prerelease versions have lower precedence than normal versions
	if v.Prerelease == "" && other.Prerelease != "" {
		return 1
	}
	if v.Prerelease != "" && other.Prerelease == "" {
		return -1
	}
	return comparePrerelease(v.Prerelease, other.Prerelease)
}

// comparePrerelease compares prerelease strings per semver spec.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func comparePrerelease(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	minLen := len(partsA)
	if len(partsB) < minLen {
		minLen = len(partsB)
	}

	for i := 0; i < minLen; i++ {
		cmp := compareIdentifier(partsA[i], partsB[i])
		if cmp != 0 {
			return cmp
		}
	}

	// All compared identifiers are equal; longer set wins
	if len(partsA) < len(partsB) {
		return -1
	}
	if len(partsA) > len(partsB) {
		return 1
	}
	return 0
}

// compareIdentifier compares two prerelease identifiers per semver spec.
func compareIdentifier(a, b string) int {
	aIsNum := isNumericIdentifier(a)
	bIsNum := isNumericIdentifier(b)

	switch {
	case aIsNum && bIsNum:
		// Both numeric: compare as integers
		return compareNumericIdentifier(a, b)
	case aIsNum:
		// a is numeric, b is not: numeric has lower precedence
		return -1
	case bIsNum:
		// b is numeric, a is not: numeric has lower precedence
		return 1
	default:
		// Both non-numeric: compare lexicographically
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
}

func isNumericIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumericIdentifier(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// LessThan returns true if v < other.
func (v Version) LessThan(other Version) bool {
	return v.Compare(other) < 0
}

// IsDevBuild returns true if this is a development build.
func IsDevBuild(version string) bool {
	v := strings.TrimSpace(version)
	return v == "" || v == "dev" || v == "none" || v == "unknown"
}
