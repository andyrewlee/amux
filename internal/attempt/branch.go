package attempt

import "strings"

// BranchName builds a branch name using prefix, issue identifier, and attempt ID.
func BranchName(prefix, identifier, attemptID string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "lin"
	}
	id := strings.TrimSpace(identifier)
	short := ShortID(attemptID)
	if short == "" {
		return strings.Join([]string{p, id}, "/")
	}
	return strings.Join([]string{p, id, short}, "/")
}
