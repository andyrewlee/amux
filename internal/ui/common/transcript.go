package common

import "strings"

// TranscriptMaxBytes caps a copied transcript at 1 MiB — large enough for a
// real agent session, small enough that pbcopy/clipboard write stays instant.
// The cap is tail-biased: the newest output is what the user wants pasted.
const TranscriptMaxBytes = 1 << 20

// TruncateTranscriptTail drops the oldest bytes of text when it exceeds
// TranscriptMaxBytes, cutting on a newline boundary so the kept transcript
// starts on a whole line. It reports whether truncation happened.
func TruncateTranscriptTail(text string) (string, bool) {
	if len(text) <= TranscriptMaxBytes {
		return text, false
	}
	kept := text[len(text)-TranscriptMaxBytes:]
	if nl := strings.IndexByte(kept, '\n'); nl >= 0 && nl+1 < len(kept) {
		kept = kept[nl+1:]
	}
	return kept, true
}
