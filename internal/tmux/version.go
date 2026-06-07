package tmux

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// TmuxVersion is a parsed tmux version. tmux numbers releases as <major>.<minor>
// with an optional trailing letter, e.g. 3.6a -> {Major:3, Minor:6, Suffix:"a"}.
type TmuxVersion struct {
	Major  int
	Minor  int
	Suffix string
	Raw    string
}

var tmuxVersionRE = regexp.MustCompile(`(\d+)\.(\d+)([a-z]*)`)

// ParseTmuxVersion extracts a TmuxVersion from `tmux -V`-style output such as
// "tmux 3.6a", "tmux next-3.5", or a bare "3.2a".
func ParseTmuxVersion(s string) (TmuxVersion, error) {
	m := tmuxVersionRE.FindStringSubmatch(s)
	if m == nil {
		return TmuxVersion{}, fmt.Errorf("unrecognized tmux version %q", strings.TrimSpace(s))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return TmuxVersion{Major: major, Minor: minor, Suffix: m[3], Raw: strings.TrimSpace(s)}, nil
}

// AtLeast reports whether v is at least major.minor with at least the given
// suffix. Major and minor are compared numerically; the suffix is compared
// lexically, so 3.6a satisfies AtLeast(3, 6, "") and AtLeast(3, 6, "a") but not
// AtLeast(3, 6, "b").
func (v TmuxVersion) AtLeast(major, minor int, suffix string) bool {
	if v.Major != major {
		return v.Major > major
	}
	if v.Minor != minor {
		return v.Minor > minor
	}
	return v.Suffix >= suffix
}

// ServerVersion returns the tmux version reported by `tmux -V`. Until this
// existed there was no version or capability detection anywhere in the codebase,
// so version-variant tmux output (e.g. a changed capture format) degraded
// silently instead of being diagnosable.
func ServerVersion() (TmuxVersion, error) {
	out, err := exec.Command("tmux", "-V").CombinedOutput()
	if err != nil {
		return TmuxVersion{}, fmt.Errorf("tmux -V: %w", err)
	}
	return ParseTmuxVersion(string(out))
}
