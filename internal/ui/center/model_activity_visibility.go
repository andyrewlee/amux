package center

import (
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	localInputEchoSuppressWindow = 500 * time.Millisecond
	bootstrapQuietGap            = tabActiveWindow
)

func (m *Model) noteVisibleActivityLocked(tab *Tab, hasMoreBuffered bool, visibleSeq uint64) (string, int64, bool) {
	if tab == nil || tab.Terminal == nil || tab.DiffViewer != nil {
		if tab != nil {
			tab.pendingVisibleOutput = false
		}
		return "", 0, false
	}
	if !tab.pendingVisibleOutput {
		return "", 0, false
	}

	now := time.Now()
	nextPending := hasMoreBuffered || tab.pendingVisibleSeq != visibleSeq
	contentVersion := tab.Terminal.ContentVersion()
	if tab.activityDigestInit && contentVersion == tab.activityContentVersion {
		tab.pendingVisibleOutput = nextPending
		return "", 0, false
	}

	digest := visibleScreenDigest(tab.Terminal)
	changed := !tab.activityDigestInit || digest != tab.activityDigest
	if !changed {
		tab.activityContentVersion = contentVersion
		tab.activityDigest = digest
		tab.activityDigestInit = true
		tab.pendingVisibleOutput = nextPending
		return "", 0, false
	}

	if tab.bootstrapActivity {
		// Explicit bootstrap phase: terminal replay/prompt redraw is visible output
		// but must not be treated as active work.
		tab.activityContentVersion = contentVersion
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
	tab.activityContentVersion = contentVersion
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

func visibleScreenDigest(term *vterm.VTerm) uint64 {
	const (
		fnvOffset64 = 14695981039346656037
		fnvPrime64  = 1099511628211
	)
	if term == nil {
		return fnvOffset64
	}

	// Use the live screen buffer, not the current viewport. If user scrolls
	// back, viewport content can stay static while live output continues.
	screen, _ := term.RenderBuffers()
	h := uint64(fnvOffset64)
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
			ru := uint32(r)
			h ^= uint64(ru & 0xFF)
			h *= fnvPrime64
			h ^= uint64((ru >> 8) & 0xFF)
			h *= fnvPrime64
			h ^= uint64((ru >> 16) & 0xFF)
			h *= fnvPrime64
			h ^= uint64((ru >> 24) & 0xFF)
			h *= fnvPrime64
		}
		h ^= uint64('\n')
		h *= fnvPrime64
	}
	return h
}
