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
	if tab == nil || tab.Terminal == nil || !m.isChatTab(tab) {
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
	if !tab.lastUserInputAt.IsZero() && now.Sub(tab.lastUserInputAt) <= localInputEchoSuppressWindow {
		// Suppressed local-echo candidate: keep prior digest baseline so a later
		// re-observation of this rendered delta can still count as activity.
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
