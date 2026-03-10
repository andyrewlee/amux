package center

import (
	"crypto/md5"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	localInputEchoSuppressWindow = 500 * time.Millisecond
	bootstrapQuietGap            = tabActiveWindow
)

func (m *Model) noteVisibleActivityLocked(tab *Tab, hasMoreBuffered bool, visibleSeq uint64) (string, int64, bool) {
	return m.noteVisibleActivityLockedWithOutput(tab, hasMoreBuffered, visibleSeq, nil)
}

func consumeSubmittedPasteEchoLocked(tab *Tab, output []byte) bool {
	if tab == nil || tab.pendingSubmitPasteEcho == "" || len(output) == 0 {
		return false
	}
	normalized := normalizeSubmittedPasteEchoOutput(output)
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, tab.pendingSubmitPasteEcho) {
		tail := strings.Trim(normalized[len(tab.pendingSubmitPasteEcho):], "\n")
		tab.pendingSubmitPasteEcho = ""
		return tail == ""
	}
	if strings.HasPrefix(tab.pendingSubmitPasteEcho, normalized) {
		tab.pendingSubmitPasteEcho = strings.TrimPrefix(tab.pendingSubmitPasteEcho, normalized)
		return true
	}
	// A visible mismatch means the redraw no longer looks like the pending paste
	// echo, so drop suppression immediately instead of risking suppression of
	// unrelated assistant output.
	tab.pendingSubmitPasteEcho = ""
	return false
}

func normalizeSubmittedPasteEchoOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	var b strings.Builder
	state := ansiActivityText
	for _, ch := range output {
		switch state {
		case ansiActivityText:
			switch ch {
			case 0x1b:
				state = ansiActivityEsc
			default:
				switch {
				case ch == '\r' || ch == '\n' || ch == '\t':
					b.WriteByte(ch)
				case ch >= 0x20 && ch != 0x7f:
					b.WriteByte(ch)
				}
			}

		case ansiActivityEsc:
			switch ch {
			case '[':
				state = ansiActivityCSI
			case ']':
				state = ansiActivityOSC
			case 'P', 'X', '^', '_':
				state = ansiActivityString
			default:
				switch {
				case ch >= 0x20 && ch <= 0x2f:
					state = ansiActivityEscSequence
				case ch >= 0x30 && ch <= 0x7e:
					state = ansiActivityText
				default:
					state = ansiActivityText
				}
			}

		case ansiActivityEscSequence:
			if ch >= 0x30 && ch <= 0x7e {
				state = ansiActivityText
			} else if ch == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityCSI:
			if ch >= 0x40 && ch <= 0x7e {
				state = ansiActivityText
			} else if ch == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityOSC:
			if ch == 0x07 {
				state = ansiActivityText
			} else if ch == 0x1b {
				state = ansiActivityOSCEsc
			}

		case ansiActivityOSCEsc:
			if ch == '\\' {
				state = ansiActivityText
			} else if ch != 0x1b {
				state = ansiActivityOSC
			}

		case ansiActivityString:
			if ch == 0x1b {
				state = ansiActivityStringEsc
			}

		case ansiActivityStringEsc:
			if ch == '\\' {
				state = ansiActivityText
			} else if ch != 0x1b {
				state = ansiActivityString
			}
		}
	}
	normalized := strings.ReplaceAll(b.String(), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "")
	return normalized
}

func (m *Model) noteVisibleActivityLockedWithOutput(
	tab *Tab,
	hasMoreBuffered bool,
	visibleSeq uint64,
	output []byte,
) (string, int64, bool) {
	if tab == nil || tab.Terminal == nil || tab.DiffViewer != nil {
		if tab != nil {
			tab.pendingVisibleOutput = false
		}
		return "", 0, false
	}
	if !tab.pendingVisibleOutput {
		return "", 0, false
	}

	digest := visibleScreenDigest(tab.Terminal)
	changed := !tab.activityDigestInit || digest != tab.activityDigest
	nextPending := hasMoreBuffered || tab.pendingVisibleSeq != visibleSeq
	if !changed {
		tab.activityDigest = digest
		tab.activityDigestInit = true
		tab.pendingVisibleOutput = nextPending
		return "", 0, false
	}

	now := time.Now()
	if tab.bootstrapActivity {
		// Explicit bootstrap phase: terminal replay/prompt redraw is visible output
		// but must not be treated as active work.
		tab.activityDigest = digest
		tab.activityDigestInit = true
		tab.pendingVisibleOutput = nextPending
		return "", 0, false
	}
	if consumeSubmittedPasteEchoLocked(tab, output) {
		tab.activityDigest = digest
		tab.activityDigestInit = true
		tab.pendingVisibleOutput = nextPending
		return "", 0, false
	}
	if !tab.lastUserInputAt.IsZero() && now.Sub(tab.lastUserInputAt) <= localInputEchoSuppressWindow {
		// Suppress local-echo candidates and keep pending so the next flush
		// cycle can re-evaluate once the echo window has passed.
		tab.pendingVisibleOutput = true
		return "", 0, false
	}
	tab.activityDigest = digest
	tab.activityDigestInit = true
	tab.pendingVisibleOutput = nextPending
	tab.lastVisibleOutput = now

	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if sessionName == "" {
		return "", 0, false
	}
	if now.Sub(tab.lastActivityTagAt) < activityTagThrottle {
		return "", 0, false
	}
	tab.lastActivityTagAt = now
	return sessionName, now.UnixMilli(), true
}

func visibleScreenDigest(term *vterm.VTerm) [16]byte {
	if term == nil {
		return md5.Sum(nil)
	}

	// Use the live screen buffer, not the current viewport. If user scrolls
	// back, viewport content can stay static while live output continues.
	screen, _ := term.RenderBuffers()
	var b strings.Builder
	for _, row := range screen {
		last := len(row) - 1
		for last >= 0 {
			cell := row[last]
			if cell.Width == 0 {
				last--
				continue
			}
			r := cell.Rune
			if r == 0 || r == ' ' {
				last--
				continue
			}
			break
		}
		for i := 0; i <= last; i++ {
			cell := row[i]
			if cell.Width == 0 {
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return md5.Sum([]byte(b.String()))
}
