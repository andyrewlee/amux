package cli

import (
	"path/filepath"
	"regexp"
	"strings"
)

func assistantTurnMatch(value, pattern string) bool {
	matched, _ := regexp.MatchString(pattern, value)
	return matched
}

func assistantTurnSuffixAfterFirstMatch(value, pattern string) string {
	re := regexp.MustCompile(pattern)
	loc := re.FindStringIndex(value)
	if loc == nil {
		return value
	}
	return value[loc[1]:]
}

func assistantTurnRemoveFirstMatch(value, pattern string) string {
	re := regexp.MustCompile(pattern)
	loc := re.FindStringIndex(value)
	if loc == nil {
		return value
	}
	return value[:loc[0]] + value[loc[1]:]
}

func assistantTurnSummaryHasStandaloneReviewReportNoun(valueLower string) bool {
	if strings.TrimSpace(valueLower) == "" {
		return false
	}
	if !assistantTurnMatch(valueLower, `(^|[^[:alnum:]_/-])(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?)([^[:alnum:]_/-]|$)`) {
		return false
	}
	return !assistantTurnMatch(valueLower, `(^|[^[:alnum:]_/-])(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?)\.[[:alnum:]_-]`)
}

func assistantTurnSummaryIsFileOnlyChangeSignal(value string) bool {
	if strings.TrimSpace(value) == "" || !assistantTurnLineHasFileSignal(value) {
		return false
	}
	trimmed := strings.TrimSpace(value)
	trimmed = regexp.MustCompile(`^[[:space:]]*(([0-9]+[.)])|[-*•]+)[[:space:]]*`).ReplaceAllString(trimmed, "")
	trimmed = regexp.MustCompile(`^[[:space:]]*([Ff]iles?|[Cc]hanges?|[Ff]ix(es)?|[Pp]atch(es)?|[Ee]dits?|[Uu]pdates?)[[:space:]]*:[[:space:]]*`).ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(strings.TrimRight(trimmed, ",:;"))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if assistantTurnMatch(lower, `^((([[:alnum:]_.-]+/)*([[:alnum:]_.-]+\.[[:alnum:]_.-]+|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?))|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)(([[:space:]]*,[[:space:]]*|[[:space:]]+and[[:space:]]+|[[:space:]]+)((([[:alnum:]_.-]+/)*([[:alnum:]_.-]+\.[[:alnum:]_.-]+|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?))|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?))*[[:space:]]*[;:.][[:space:]]*(no issues?|no problems?|no concerns?|no regressions?|no changes?[[:space:]]+(required|needed)|no edits?[[:space:]]+(required|needed)|no fixes?[[:space:]]+(required|needed))([^[:alpha:]]|$)`) {
		return false
	}
	return assistantTurnMatch(trimmed, `^((([[:alnum:]_.-]+/)*([[:alnum:]_.-]+\.[[:alnum:]_.-]+|Dockerfile|Makefile|README(\.[[:alnum:]_.-]+)?))|Dockerfile|Makefile|README(\.[[:alnum:]_.-]+)?)(([[:space:]]*,[[:space:]]*|[[:space:]]+and[[:space:]]+|[[:space:]]+)((([[:alnum:]_.-]+/)*([[:alnum:]_.-]+\.[[:alnum:]_.-]+|Dockerfile|Makefile|README(\.[[:alnum:]_.-]+)?))|Dockerfile|Makefile|README(\.[[:alnum:]_.-]+)?))*([[:space:]]*[;:.][[:space:]]*.+)?$`)
}

func assistantTurnSummaryHasExplicitNoopFilePhrase(value string) bool {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "no files changed"),
		strings.Contains(lower, "no file changed"),
		strings.Contains(lower, "no changed files"),
		strings.Contains(lower, "no changed file"),
		strings.Contains(lower, "no files modified"),
		strings.Contains(lower, "no file modified"),
		strings.Contains(lower, "no modified files"),
		strings.Contains(lower, "no modified file"),
		strings.Contains(lower, "files unchanged"),
		strings.Contains(lower, "file unchanged"):
		return true
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? changed|changed files?|files? modified|modified files?)[[:space:]]*[:=-]?[[:space:]]*(none|zero|0)([^[:alpha:]]|$)`) ||
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(none|zero|0)[[:space:]]+(files? changed|changed files?|files? modified|modified files?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasBroadEditVerb(value string) bool {
	return assistantTurnMatch(strings.ToLower(value), `(^|[^[:alnum:]_./-])(added|adjust(ed|ing|s)?|applied|changed|clarif(y|ied|ies|ying)|clean(ed|ing|s)?|consolid(ate|ated|ates|ating)|correct(ed|ing|s)?|created|deleted|edited|fixed|generat(e|ed|es|ing)|harden(ed|ing|s)?|implemented|improved|migrat(e|ed|es|ing)|moderniz(e|ed|es|ing)|modified|optimiz(e|ed|es|ing)|overhaul(ed|ing|s)?|patched|polish(ed|es|ing)?|prun(e|ed|es|ing)|refactor|refactored|refactoring|removed|renamed|reorganiz(e|ed|es|ing)|resolv(e|ed|es|ing)|restructur(e|ed|es|ing)|revis(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|simplif(y|ied|ies|ying)|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?|touched|trim(med|ming|s)?|tweak(ed|ing|s)?|updated|wrote)([^[:alnum:]_./-]|$)`)
}

func assistantTurnSummaryHasExplicitAppliedEditVerb(value string) bool {
	lower := strings.ToLower(value)
	if assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(added|adjust(ed|ing|s)?|applied|clarif(y|ied|ies|ying)|clean(ed|ing|s)?|consolid(ate|ated|ates|ating)|correct(ed|ing|s)?|deleted|edited|fixed|harden(ed|ing|s)?|implemented|improved|migrat(e|ed|es|ing)|moderniz(e|ed|es|ing)|optimiz(e|ed|es|ing)|overhaul(ed|ing|s)?|patched|polish(ed|es|ing)?|prun(e|ed|es|ing)|removed|renamed|reorganiz(e|ed|es|ing)|resolv(e|ed|es|ing)|restructur(e|ed|es|ing)|revis(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|simplif(y|ied|ies|ying)|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?|touched|trim(med|ming|s)?|tweak(ed|ing|s)?|updated)([^[:alnum:]_./-]|$)`) {
		return true
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])changed([^[:alpha:]]|$)`) &&
		!assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? changed|changed files?)([^[:alpha:]]|$)`) {
		return true
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])modified([^[:alpha:]]|$)`) &&
		!assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? modified|modified files?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasReviewableNonfileEditVerb(value string) bool {
	lower := strings.ToLower(value)
	if assistantTurnSummaryHasExplicitNoopFilePhrase(lower) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(added|adjust(ed|ing|s)?|applied|clarif(y|ied|ies|ying)|clean(ed|ing|s)?|consolid(ate|ated|ates|ating)|correct(ed|ing|s)?|created|deleted|fixed|generated|harden(ed|ing|s)?|implemented|introduced|migrat(e|ed|es|ing)|moderniz(e|ed|es|ing)|modified|optimiz(e|ed|es|ing)|overhaul(ed|ing|s)?|patched|polish(ed|es|ing)?|prun(e|ed|es|ing)|refactor|refactored|refactoring|removed|renamed|reorganiz(e|ed|es|ing)|resolv(e|ed|es|ing)|restructur(e|ed|es|ing)|revis(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|scaffold(ed|ing|s)?|simplif(y|ied|ies|ying)|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?|trim(med|ming|s)?|tweak(ed|ing|s)?|updated)([^[:alnum:]_./-]|$)`)
}

func assistantTurnSummaryHasStrongNonfileEditVerb(value string) bool {
	return assistantTurnMatch(strings.ToLower(value), `(^|[^[:alnum:]_./-])(clean(ed|ing|s)?|consolid(ate|ated|ates|ating)|correct(ed|ing|s)?|fixed|harden(ed|ing|s)?|implemented|migrat(e|ed|es|ing)|moderniz(e|ed|es|ing)|optimiz(e|ed|es|ing)|overhaul(ed|ing|s)?|patched|prun(e|ed|es|ing)|refactor|refactored|refactoring|removed|renamed|reorganiz(e|ed|es|ing)|resolv(e|ed|es|ing)|restructur(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?)([^[:alnum:]_./-]|$)`)
}

func assistantTurnSummaryHasEditVerbWithFilenameishTarget(value string) bool {
	lower := strings.ToLower(value)
	switch {
	case assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(added|created|generated|wrote)[[:space:]]+(tests?|test[[:space:]]+cases?|test[[:space:]]+coverage)[[:space:]]+(for|in|to)[[:space:]]+((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)([^[:alnum:]_./-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(moved|move(d|s|ing)?|renamed|rename(d|s|ing)?)([^[:alnum:]_./-]|$).*((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)[[:space:]]+(to|into|as)[[:space:]]+((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)([^[:alnum:]_./-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(replaced|replace(d|s|ing)?)([^[:alnum:]_./-]|$).*((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)[[:space:]]+with[[:space:]]+((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)([^[:alnum:]_./-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(added|adjusted|changed|clarified|created|deleted|edited|fixed|generated|improved|introduced|modified|patched|polished|removed|replaced|revised|scaffolded|simplified|touched|updated|wrote)([^[:alnum:]_./-]|$).*(to|in|into|within|on|from|with)[[:space:]]+((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|dockerfile|makefile|readme(\.[[:alnum:]_.-]+)?)([^[:alnum:]_./-]|$)`):
		return true
	}
	for _, target := range assistantTurnFilenameTokenRE.FindAllString(lower, -1) {
		target = strings.TrimRight(target, ".,:;!?)]}\"'")
		if target == "" {
			continue
		}
		if assistantTurnIsSpecialFilename(target) || strings.Contains(target, "/") {
			return assistantTurnSummaryHasBroadEditVerb(value)
		}
		if !strings.Contains(target, ".") || assistantTurnIsVersionLike(target) {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(target)), ".")
		if assistantTurnIsLikelyDomain(target) && !assistantTurnIsEnvLike(target) {
			continue
		}
		if assistantTurnIsLikelyDomain(target) && !assistantTurnAllowedFileExt(ext) {
			continue
		}
		return assistantTurnSummaryHasBroadEditVerb(value)
	}
	return false
}

func assistantTurnSummaryHasFileBackedReportWrite(value string) bool {
	if strings.TrimSpace(value) == "" || !assistantTurnLineHasFileSignal(value) {
		return false
	}
	lower := strings.ToLower(value)
	reportFileRE := `((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|makefile|readme(\.[[:alnum:]_.-]+)?)`
	switch {
	case assistantTurnMatch(lower, `(^|[^[:alpha:]])(saved|wrote|created|documented|captured|drafted|prepared|reported)([^[:alpha:]]|$).*(to|into|in|as)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(saved|wrote|created|documented|captured|drafted|prepared|reported)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(updated|modified)([^[:alpha:]]|$).*(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?).*(to|into|in|as)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(added|improved)([^[:alpha:]]|$).*(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?).*(to|into|in|as)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$)`),
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(updated|modified)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$).*(with|for)[[:space:]]+(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?)`),
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(added|improved)[[:space:]]+(the[[:space:]]+)?(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?).*(to|into|in|as)[[:space:]]+`+reportFileRE+`([^[:alnum:]_/-]|$)`):
		return true
	default:
		return false
	}
}

func assistantTurnSummaryHasFileBackedNounEditClaim(value string) bool {
	if strings.TrimSpace(value) == "" || !assistantTurnLineHasFileSignal(value) {
		return false
	}
	lower := strings.ToLower(value)
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(patch|patches|fix|fixes|change|changes|edit|edits|update|updates|refactor|refactors|rewrite|rewrites)([[:space:]]+made)?[[:space:]]+(for|in|to|on|across|within|touching)([^[:alpha:]]|$).*(were|was|are|is)?[[:space:]]*(required|needed).*(applied|made|implemented|completed|addressed)([^[:alpha:]]|$)`) {
		return true
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(patch|patches|fix|fixes|change|changes|edit|edits|update|updates|refactor|refactors|rewrite|rewrites)([[:space:]]+made)?[[:space:]]+(for|in|to|on|across|within|touching)([^[:alpha:]]|$).*(were|was|are|is)?[[:space:]]*(required|needed)([^[:alpha:]]|$)`) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(patch|patches|fix|fixes|change|changes|edit|edits|update|updates|refactor|refactors|rewrite|rewrites)([[:space:]]+made)?[[:space:]]+(for|in|to|on|across|within|touching)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasAppliedNounEditClaim(value string) bool {
	return assistantTurnMatch(strings.ToLower(value), `(^|[^[:alpha:]])(made|applied|implemented|completed|addressed)[[:space:]]+(the[[:space:]]+)?((required|requested|necessary|needed)[[:space:]]+)?(changes?|edits?|fix(es)?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasArtifactCreationClaim(value string) bool {
	return assistantTurnMatch(strings.ToLower(value), `(^|[^[:alpha:]])(added|created|generated|scaffold(ed|ing|s)?|wrote)([^[:alpha:]]|$).*(tests?|test[[:space:]]+cases?|test[[:space:]]+coverage|files?|fixtures?|mocks?|stubs?|snapshots?|docs?|documentation|readme|scripts?|configs?|configuration|components?|handlers?|endpoints?|modules?|packages?|schemas?|assets?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasExplicitWorkspaceEditClaim(value string) bool {
	return assistantTurnSummaryHasFileBackedReportWrite(value) ||
		assistantTurnSummaryHasFileBackedNounEditClaim(value) ||
		assistantTurnSummaryHasAppliedNounEditClaim(value) ||
		assistantTurnSummaryHasArtifactCreationClaim(value) ||
		assistantTurnSummaryHasEditVerbWithFilenameishTarget(value)
}

func assistantTurnSummaryIsNoncodeEditPhrase(value string) bool {
	if strings.TrimSpace(value) == "" || assistantTurnLineHasFileSignal(value) {
		return false
	}
	lower := strings.ToLower(value)
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(adjusted|changed|clarified|improved|modified|refined|revised|updated)([^[:alpha:]]|$).*(approach|architecture|design|direction|plan|recommendations?|strategy)([^[:alpha:]]|$)`) {
		return true
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(adjusted|changed|clarified|improved|modified|refined|revised|updated)([^[:alpha:]]|$).*(analysis|findings?|notes?|remediation|summary|summaries?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryIsReviewReportOnly(value string) bool {
	if strings.TrimSpace(value) == "" || assistantTurnSummaryHasFileBackedReportWrite(value) || assistantTurnSummaryHasFileBackedNounEditClaim(value) {
		return false
	}
	lower := strings.ToLower(value)
	if !assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$)`) {
		return false
	}
	reportOnlyPattern := `(^|[^[:alpha:]])(added|captured|clarif(y|ied|ies|ying)|created|documented|drafted|improved|modified|prepared|refactor|refactored|refactoring|reported|revis(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|simplif(y|ied|ies|ying)|updated|wrote)[[:space:]]+(a[[:space:]]+|an[[:space:]]+|the[[:space:]]+)?(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?)([^[:alpha:]]|$)`
	if assistantTurnSummaryHasStandaloneReviewReportNoun(lower) &&
		assistantTurnMatch(lower, reportOnlyPattern) &&
		!assistantTurnSummaryHasExplicitWorkspaceEditClaim(assistantTurnRemoveFirstMatch(lower, reportOnlyPattern)) {
		return true
	}
	if assistantTurnSummaryHasExplicitAppliedEditVerb(value) || assistantTurnSummaryHasReviewableNonfileEditVerb(value) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(created|wrote|reported|documented|captured|drafted|prepared)([^[:alpha:]]|$)`) &&
		assistantTurnSummaryHasStandaloneReviewReportNoun(lower)
}

func assistantTurnSummaryIsReviewOnlyModifiedFileScan(value string) bool {
	if strings.TrimSpace(value) == "" || assistantTurnSummaryHasFileBackedReportWrite(value) || assistantTurnSummaryHasFileBackedNounEditClaim(value) {
		return false
	}
	lower := strings.ToLower(value)
	trimmed := regexp.MustCompile(`^[[:space:]]*(([0-9]+[.)])|[-*•]+)[[:space:]]*`).ReplaceAllString(lower, "")
	noopTail := `(no issues?([[:space:]]+found)?|no problems?([[:space:]]+found)?|no concerns?([[:space:]]+found)?|no regressions?([[:space:]]+found)?|no changes?[[:space:]]+(required|needed)|no edits?[[:space:]]+(required|needed)|no fixes?[[:space:]]+(required|needed))`
	if !assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$)`) ||
		!assistantTurnMatch(lower, noopTail) {
		return false
	}
	if !assistantTurnMatch(trimmed, `^[[:space:]]*(review|reviewed|reviewing)[[:space:]]+(the[[:space:]]+)?(modified|updated)[[:space:]]+((([[:alnum:]_.-]+/)*[[:alnum:]_.-]+\.[[:alnum:]_.-]+)|makefile|readme(\.[[:alnum:]_.-]+)?)([[:space:]]*[;:.][[:space:]]*`+noopTail+`([^[:alpha:]]|$))?[[:space:]]*$`) {
		return false
	}
	return true
}

func assistantTurnSummaryIsReviewOnlyChangedFilesScan(value string) bool {
	if strings.TrimSpace(value) == "" || assistantTurnSummaryHasFileBackedReportWrite(value) || assistantTurnSummaryHasFileBackedNounEditClaim(value) {
		return false
	}
	lower := strings.ToLower(value)
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$)`) &&
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(changed files?|files? changed|modified files?|files? modified|updated files?|files? updated|changed code|modified code|updated code|changed implementation|modified implementation|updated implementation)([^[:alpha:]]|$)`) {
		stripped := regexp.MustCompile(`(^|[^[:alpha:]])(changed files?|files? changed|modified files?|files? modified|updated files?|files? updated|changed code|modified code|updated code|changed implementation|modified implementation|updated implementation)([^[:alpha:]]|$)`).ReplaceAllString(lower, " ")
		stripped = regexp.MustCompile(`(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$)`).ReplaceAllString(stripped, " ")
		if assistantTurnSummaryHasEditVerbWithFilenameishTarget(stripped) ||
			assistantTurnSummaryHasArtifactCreationClaim(stripped) ||
			assistantTurnSummaryHasAppliedNounEditClaim(stripped) {
			return false
		}
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$).*(updated|modified)[[:space:]]+(files?|code|implementation)([^[:alpha:]]|$).*(no issues?|no problems?|no concerns?|no regressions?|no changes?[[:space:]]+(required|needed)|no edits?[[:space:]]+(required|needed)|no fixes?[[:space:]]+(required|needed))`) {
		return true
	}
	if assistantTurnSummaryHasExplicitAppliedEditVerb(value) || assistantTurnSummaryHasAppliedNounEditClaim(value) ||
		assistantTurnMatch(lower, `(^|[^[:alnum:]_./-])(refactor|refactored|refactoring)([^[:alnum:]_./-]|$)`) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(changed files?|files? changed|modified files?|files? modified)([^[:alpha:]]|$)`) &&
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryHasFileChangePhrase(value string) bool {
	lower := strings.ToLower(value)
	if assistantTurnSummaryHasExplicitNoopFilePhrase(lower) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? changed|changed files?|files? modified|modified files?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryIsNonfileReportOnly(value string) bool {
	if strings.TrimSpace(value) == "" || assistantTurnLineHasFileSignal(value) {
		return false
	}
	lower := strings.ToLower(value)
	return assistantTurnSummaryHasStandaloneReviewReportNoun(lower) &&
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(added|applied|created|clarif(y|ied|ies|ying)|implemented|improved|refactor|refactored|refactoring|revis(e|ed|es|ing)|rework(ed|ing|s)?|rewrite|rewrites|rewrote|simplif(y|ied|ies|ying)|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?|wrote|reported|documented|captured|drafted|prepared|updated|modified)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryIsReviewObservedChangeFinding(value string) bool {
	lower := strings.ToLower(value)
	if strings.TrimSpace(value) == "" || assistantTurnSummaryHasFileBackedReportWrite(value) || assistantTurnSummaryHasFileBackedNounEditClaim(value) ||
		assistantTurnSummaryHasEditVerbWithFilenameishTarget(value) || assistantTurnSummaryHasAppliedNounEditClaim(value) ||
		!assistantTurnSummaryHasStrongNonfileEditVerb(value) {
		return false
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$).*(removed|changed|modified|updated)([^[:alpha:]]|$).*(field|fields|property|properties|key|keys|param|params|parameter|parameters|column|columns|value|values|response|request|payload|schema|contract|header|headers)([^[:alpha:]]|$)`) {
		return true
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(fixed|removed|resolved|addressed|patched|correct(ed|ing|s)?|clean(ed|ing|s)?|harden(ed|ing|s)?|implemented|improved|modified|optimized|rework(ed|ing|s)?|rewrite|rewrites|rewrote|streamlin(e|ed|es|ing)|tighten(ed|ing|s)?|updated)([^[:alpha:]]|$).*((the|a|an)[[:space:]]+)?(compatibility risk|compatibility issue|issue|issues|risk|risks|problem|problems|concern|concerns|finding|findings)([^[:alpha:]]|$)`) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(breaking change|breaking changes|backward incompatible|backwards incompatible|compatibility risk|compatibility issue|risk|risks|issue|issues|problem|problems|concern|concerns|finding|findings)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryIsTargetedReportOnly(value string) bool {
	if strings.TrimSpace(value) == "" || !assistantTurnLineHasFileSignal(value) || assistantTurnSummaryHasFileBackedReportWrite(value) {
		return false
	}
	lower := strings.ToLower(value)
	return assistantTurnSummaryHasStandaloneReviewReportNoun(lower) &&
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(created|wrote|reported|documented|captured|drafted|prepared|added|improved|updated|modified)([^[:alpha:]]|$).*(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?).*(for|about|on)[[:space:]]+`) &&
		!assistantTurnSummaryHasExplicitWorkspaceEditClaim(assistantTurnSuffixAfterFirstMatch(lower, `(^|[^[:alpha:]])(created|wrote|reported|documented|captured|drafted|prepared|added|improved|updated|modified)([^[:alpha:]]|$).*(analysis|findings?|notes?|plan|recommendations?|remediation|report|reports?|summary|summaries?).*(for|about|on)[[:space:]]+`))
}

func assistantTurnContextHasReviewableFileChangePhrase(value string) bool {
	lower := strings.ToLower(value)
	if strings.TrimSpace(value) == "" || assistantTurnSummaryHasExplicitNoopFilePhrase(lower) {
		return false
	}
	switch {
	case strings.Contains(lower, "no files changed"),
		strings.Contains(lower, "no file changed"),
		strings.Contains(lower, "no files modified"),
		strings.Contains(lower, "no file modified"),
		strings.Contains(lower, "no modified files"),
		strings.Contains(lower, "no changed files"),
		strings.Contains(lower, "files unchanged"),
		strings.Contains(lower, "file unchanged"):
		return false
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? changed|changed files?|files? modified|modified files?)[[:space:]]*[:=-]?[[:space:]]*none([^[:alpha:]]|$)`) {
		return false
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(files? changed|changed files?|files? modified|modified files?)([^[:alpha:]]|$)`) {
		return true
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])review([^[:alpha:]]|$).*(patch(ed)?|refactor(ed|ing)?|rewrite|rewrites|rewrote)([^[:alpha:]]|$)`) {
		if assistantTurnMatch(lower, `(patch(ed)?|refactor(ed|ing)?|rewrite|rewrites|rewrote)([^[:alpha:]]|$).*(required|needed)([^[:alpha:]]|$)`) {
			return false
		}
		if assistantTurnSummaryHasStandaloneReviewReportNoun(lower) &&
			assistantTurnMatch(lower, `(refactor(ed|ing)?|rewrite|rewrites|rewrote)([^[:alpha:]]|$).*(analysis|findings?|notes?|plan|recommendations?|remediation)([^[:alpha:]]|$)`) {
			return false
		}
		return true
	}
	if assistantTurnSummaryHasStandaloneReviewReportNoun(lower) &&
		assistantTurnMatch(lower, `(^|[^[:alpha:]])(modified|updated)[[:space:]]+(implementation|logic|design|approach|architecture)[[:space:]]+(analysis|findings?|notes?|plan|recommendations?|remediation)([^[:alpha:]]|$)`) {
		return false
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(modified|updated)[[:space:]]+(files?|code|changes?|implementation|logic|modules?|packages?|functions?|methods?|endpoints?|handlers?|components?)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryIsReviewFindingsOnly(value string) bool {
	lower := strings.ToLower(value)
	if assistantTurnSummaryIsReviewReportOnly(value) || assistantTurnSummaryIsReviewOnlyModifiedFileScan(value) || assistantTurnSummaryIsReviewOnlyChangedFilesScan(value) {
		return true
	}
	if assistantTurnSummaryHasExplicitAppliedEditVerb(value) || assistantTurnSummaryHasAppliedNounEditClaim(value) ||
		assistantTurnSummaryHasArtifactCreationClaim(value) || assistantTurnSummaryHasFileBackedReportWrite(value) ||
		assistantTurnSummaryHasFileBackedNounEditClaim(value) {
		return false
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$).*(finding|findings)([^[:alpha:]]|$)`) {
		return true
	}
	return assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|reviewing)([^[:alpha:]]|$).*(changes?|edits?|fix(es)?|refactor(ed|ing)?|update(d|s)?|rewrite|rewrites|rewrote|rewritten|modified|patched?)[[:space:]]+(required|needed)([^[:alpha:]]|$)`)
}

func assistantTurnSummaryDescribesWorkspaceEdits(value string) bool {
	lower := strings.ToLower(value)
	switch {
	case strings.TrimSpace(value) == "":
		return false
	case assistantTurnSummaryIsReviewFindingsOnly(value),
		assistantTurnSummaryIsNonfileReportOnly(value),
		assistantTurnSummaryIsTargetedReportOnly(value),
		assistantTurnSummaryIsReviewObservedChangeFinding(value):
		return false
	case assistantTurnSummaryIsFileOnlyChangeSignal(value),
		assistantTurnSummaryHasFileBackedReportWrite(value),
		assistantTurnSummaryHasAppliedNounEditClaim(value),
		assistantTurnSummaryHasArtifactCreationClaim(value),
		assistantTurnSummaryHasFileChangePhrase(value):
		return true
	}
	switch {
	case strings.Contains(lower, "summary of changes:"),
		strings.Contains(lower, "summary of changes -"),
		strings.Contains(lower, "summary of fixes:"),
		strings.Contains(lower, "summary of fixes -"):
		return assistantTurnLineHasFileSignal(value)
	}
	if assistantTurnSummaryHasReviewableNonfileEditVerb(value) {
		if assistantTurnMatch(lower, `(^|[^[:alpha:]])(files?|code|changes?|implementation|logic|modules?|packages?|functions?|methods?|endpoints?|handlers?|components?|parsers?|outputs?|tests?)([^[:alpha:]]|$)`) {
			return true
		}
		if assistantTurnSummaryHasStrongNonfileEditVerb(value) {
			return true
		}
	}
	if assistantTurnMatch(lower, `(^|[^[:alpha:]])(review|reviewed|finding|findings)([^[:alpha:]]|$).*(changes?|edits?|fix(es)?)[[:space:]]+(required|needed)`) {
		return false
	}
	switch {
	case strings.HasPrefix(lower, "changes required"),
		strings.HasPrefix(lower, "change required"),
		strings.HasPrefix(lower, "changes needed"),
		strings.HasPrefix(lower, "change needed"),
		strings.HasPrefix(lower, "edits required"),
		strings.HasPrefix(lower, "edit required"),
		strings.HasPrefix(lower, "edits needed"),
		strings.HasPrefix(lower, "edit needed"),
		strings.HasPrefix(lower, "fixes required"),
		strings.HasPrefix(lower, "fix required"),
		strings.HasPrefix(lower, "fixes needed"),
		strings.HasPrefix(lower, "fix needed"):
		return false
	case strings.Contains(lower, "no changes required"),
		strings.Contains(lower, "no change required"),
		strings.Contains(lower, "no changes needed"),
		strings.Contains(lower, "no change needed"),
		strings.Contains(lower, "no changes were required"),
		strings.Contains(lower, "no changes were needed"),
		strings.Contains(lower, "no changes made"),
		strings.Contains(lower, "no change made"),
		strings.Contains(lower, "no edits required"),
		strings.Contains(lower, "no edit required"),
		strings.Contains(lower, "no edits needed"),
		strings.Contains(lower, "no edit needed"),
		strings.Contains(lower, "no edits made"),
		strings.Contains(lower, "no edit made"),
		strings.Contains(lower, "no fixes required"),
		strings.Contains(lower, "no fix required"),
		strings.Contains(lower, "no fixes needed"),
		strings.Contains(lower, "no fix needed"),
		strings.Contains(lower, "no fixes made"),
		strings.Contains(lower, "no fix made"),
		strings.Contains(lower, "no file changes"):
		return false
	default:
		return false
	}
}

func assistantTurnSummaryClaimsWorkspaceEdits(value string) bool {
	if assistantTurnSummaryHasExplicitNoopFilePhrase(value) ||
		assistantTurnSummaryIsReviewFindingsOnly(value) ||
		assistantTurnSummaryIsTargetedReportOnly(value) ||
		assistantTurnSummaryIsNonfileReportOnly(value) ||
		assistantTurnSummaryIsReviewObservedChangeFinding(value) ||
		assistantTurnSummaryIsNoncodeEditPhrase(value) {
		return false
	}
	switch {
	case assistantTurnSummaryHasExplicitAppliedEditVerb(value),
		assistantTurnSummaryHasFileChangePhrase(value),
		assistantTurnSummaryHasAppliedNounEditClaim(value),
		assistantTurnSummaryHasArtifactCreationClaim(value),
		assistantTurnSummaryDescribesWorkspaceEdits(value),
		assistantTurnSummaryHasFileBackedNounEditClaim(value),
		assistantTurnSummaryHasStrongNonfileEditVerb(value),
		assistantTurnSummaryHasEditVerbWithFilenameishTarget(value):
		return true
	default:
		return false
	}
}
