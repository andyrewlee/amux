package center

import "github.com/andyrewlee/amux/internal/ui/compositor"

// learnStableChatCursorLocked is the stable chat-cursor learning state machine,
// run when the app does not own the cursor (live cursor visible, not alt-screen).
// It computes the current cursor trust policy from the materialized snapshot and
// then hands the decision flags to applyLearnedChatCursorLocked, which updates
// the stable-cursor state and points snap's cursor at the chosen position.
//
// This lives in the render path because it depends on the fully materialized
// snapshot; the snapshot cache key in TerminalLayerWithCursorOwner prevents
// repeated View passes from churning this state when its inputs are unchanged.
// Mutates snap and tab.stable* in place; the caller must hold tab.mu.
func (m *Model) learnStableChatCursorLocked(
	tab *Tab,
	snap *compositor.VTermSnapshot,
	version uint64,
	liveCursorX, liveCursorY int,
	recentLocalInput, restrictCursor, visibleOutputActive, cursorOutputActive bool,
) {
	snap.CursorHidden = false
	trustFullViewport := !restrictCursor
	if restrictCursor {
		tab.lastRestrictedVersion = version
		if cursorOutputActive && !visibleOutputActive && tab.stableCursorSet {
			tab.pendingIdleCursorRelearn = true
		}
	}
	versionChangedFromStable := version != tab.stableCursorVersion
	idleSameVersionRelearn := trustFullViewport &&
		tab.stableCursorSet &&
		!recentLocalInput &&
		version == tab.lastRestrictedVersion &&
		(tab.pendingIdleCursorRelearn || tab.stableCursorVersion == 0) &&
		hasChatCursorContextNearPosition(snap, liveCursorY)
	plausibleInitialCursor := isPlausibleInitialChatCursor(snap, liveCursorX, liveCursorY)
	initialFullViewportLearn := !tab.stableCursorSet && plausibleInitialCursor
	learnFullViewport := trustFullViewport &&
		(initialFullViewportLearn ||
			(tab.stableCursorSet && recentLocalInput) ||
			idleSameVersionRelearn ||
			(versionChangedFromStable && version != tab.lastRestrictedVersion))
	liveCursorVisible := isChatInputCursorPosition(snap, liveCursorX, liveCursorY, trustFullViewport)
	liveCursorDisplayable := liveCursorVisible &&
		(!restrictCursor || !isSuspiciousBottomEdgeCornerCursor(snap, liveCursorX, liveCursorY)) &&
		(tab.stableCursorSet || !trustFullViewport || plausibleInitialCursor)
	liveCursorRenderable := isRenderableChatCursorPosition(
		snap,
		liveCursorX,
		liveCursorY,
		learnFullViewport,
		recentLocalInput,
	)
	storedCursorInViewport := tab.stableCursorSet &&
		isStoredChatCursorPosition(snap, tab.stableCursorX, tab.stableCursorY, true)
	storedCursorVisible := tab.stableCursorSet &&
		isStoredChatCursorPosition(snap, tab.stableCursorX, tab.stableCursorY, trustFullViewport)

	applyLearnedChatCursorLocked(tab, snap, version, liveCursorX, liveCursorY,
		liveCursorRenderable, liveCursorDisplayable, trustFullViewport,
		storedCursorVisible, storedCursorInViewport)
}

// applyLearnedChatCursorLocked updates the stable-cursor state from the computed
// trust flags and then places snap's cursor: at the stable position when set,
// left at the live position when displayable, otherwise hidden. Caller holds
// tab.mu.
func applyLearnedChatCursorLocked(
	tab *Tab,
	snap *compositor.VTermSnapshot,
	version uint64,
	liveCursorX, liveCursorY int,
	liveCursorRenderable, liveCursorDisplayable, trustFullViewport,
	storedCursorVisible, storedCursorInViewport bool,
) {
	if liveCursorRenderable {
		tab.stableCursorSet = true
		tab.stableCursorX = liveCursorX
		tab.stableCursorY = liveCursorY
		tab.stableCursorVersion = version
		tab.pendingIdleCursorRelearn = false
	} else if tab.stableCursorSet &&
		tab.stableCursorVersion == 0 &&
		trustFullViewport &&
		storedCursorVisible {
		tab.stableCursorVersion = version
	} else if tab.stableCursorSet &&
		!storedCursorInViewport {
		tab.stableCursorSet = false
		tab.stableCursorVersion = 0
		tab.pendingIdleCursorRelearn = false
	}

	switch {
	case tab.stableCursorSet:
		snap.CursorX = tab.stableCursorX
		snap.CursorY = tab.stableCursorY
	case liveCursorDisplayable:
		// Leave the live cursor in place until we learn a stable input-band position.
	default:
		snap.ShowCursor = false
	}
}
