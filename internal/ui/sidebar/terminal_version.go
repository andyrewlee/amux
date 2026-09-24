package sidebar

import (
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TabBarVersion fingerprints every input that shapes the terminal tab bar:
// workspace presence, the tab list's identity/name/disconnect state, the
// active index, and the theme. Tab state mutates on async attach/exit paths
// rather than through Update, so this is a computed fingerprint (the same
// pattern as center.ActivityVersion), not a mutation counter.
func (m *TerminalModel) TabBarVersion() uint64 {
	fp := common.FoldFingerprintBool(0, m.workspace == nil)
	fp = common.FoldFingerprint(fp, m.stylesRev)
	fp = common.FoldFingerprint(fp, uint64(m.getActiveTabIdx()+1))
	for _, tab := range m.getTabs() {
		fp = common.FoldFingerprintString(fp, string(tab.ID))
		fp = common.FoldFingerprintString(fp, tab.Name)
		disconnected := false
		if tab.State != nil {
			tab.State.mu.Lock()
			disconnected = tab.State.Detached || !tab.State.Running
			tab.State.mu.Unlock()
		}
		fp = common.FoldFingerprintBool(fp, tab.State == nil)
		fp = common.FoldFingerprintBool(fp, disconnected)
	}
	return fp
}

// StatusLineVersion fingerprints the inputs to StatusLine: which terminal is
// active, its lifecycle flags, and its scroll state. ViewOffset can move
// without a vterm version bump (anchored scrollback adjustments), so the
// offset/total fold directly under ts.mu.
func (m *TerminalModel) StatusLineVersion() uint64 {
	fp := common.FoldFingerprint(0, m.stylesRev)
	ts := m.getTerminal()
	fp = common.FoldFingerprintBool(fp, ts == nil)
	if ts == nil {
		return fp
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	fp = common.FoldFingerprintBool(fp, ts.VTerm != nil)
	fp = common.FoldFingerprintBool(fp, ts.Running)
	fp = common.FoldFingerprintBool(fp, ts.Detached)
	if ts.VTerm != nil {
		fp = common.FoldFingerprintBool(fp, ts.VTerm.IsScrolled())
		offset, total := ts.VTerm.GetScrollInfo()
		fp = common.FoldFingerprint(fp, uint64(offset))
		fp = common.FoldFingerprint(fp, uint64(total))
	}
	return fp
}

// HelpVersion fingerprints the inputs to HelpLines other than the compose
// geometry (width/height ride in the gate's geom key): keymap-hint
// visibility, whether multiple tabs exist, whether a terminal exists, and the
// theme.
func (m *TerminalModel) HelpVersion() uint64 {
	fp := common.FoldFingerprint(0, m.stylesRev)
	fp = common.FoldFingerprintBool(fp, m.showKeymapHints)
	fp = common.FoldFingerprintBool(fp, m.HasMultipleTabs())
	ts := m.getTerminal()
	fp = common.FoldFingerprintBool(fp, ts != nil)
	if ts != nil {
		ts.mu.Lock()
		fp = common.FoldFingerprintBool(fp, ts.VTerm != nil)
		ts.mu.Unlock()
	}
	return fp
}

// TabBarBuildCount reports TabBarView invocations; test instrumentation.
func (m *TerminalModel) TabBarBuildCount() uint64 { return m.tabBarBuilds }

// StatusLineBuildCount reports StatusLine invocations; test instrumentation.
func (m *TerminalModel) StatusLineBuildCount() uint64 { return m.statusBuilds }

// HelpBuildCount reports HelpLines invocations; test instrumentation.
func (m *TerminalModel) HelpBuildCount() uint64 { return m.helpBuilds }
