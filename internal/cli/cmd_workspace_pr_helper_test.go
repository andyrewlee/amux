package cli

import "strings"

func helperListPRsForHead(prs map[string]ghPRView, head string) []ghPRView {
	head = strings.TrimSpace(head)
	if head == "" {
		return nil
	}
	matches := make([]ghPRView, 0, len(prs))
	for _, pr := range prs {
		candidate := strings.TrimSpace(pr.HeadRefName)
		if candidate == head {
			matches = append(matches, pr)
			continue
		}
		if _, branch, ok := strings.Cut(candidate, ":"); ok && strings.TrimSpace(branch) == head {
			matches = append(matches, pr)
		}
	}
	return matches
}

func helperSplitHeadSelector(head string) (string, string) {
	head = strings.TrimSpace(head)
	if head == "" {
		return "", ""
	}
	if owner, branch, ok := strings.Cut(head, ":"); ok {
		return strings.TrimSpace(owner), strings.TrimSpace(branch)
	}
	return "", head
}
