package tmux

import (
	"strings"
	"testing"
)

func TestAttachOnlyClientCommand(t *testing.T) {
	opts := Options{ServerName: "amux-test", ConfigPath: "/tmp/cfg"}
	cmd := AttachOnlyClientCommand("amux-ws-run", opts)

	if !strings.Contains(cmd, "attach -dt") {
		t.Errorf("missing attach -dt: %s", cmd)
	}
	if !strings.Contains(cmd, "-L 'amux-test'") {
		t.Errorf("missing server flag: %s", cmd)
	}
	for _, forbidden := range []string{"new-session", "set-option -t", "@amux_tab", "@amux_type", "has-session"} {
		if strings.Contains(cmd, forbidden) {
			t.Errorf("attach-only command contains %q: %s", forbidden, cmd)
		}
	}
	// terminal-features is a SERVER option (-s), not a session mutation.
	if !strings.Contains(cmd, "set-option -s") {
		t.Errorf("missing terminal-features sync set: %s", cmd)
	}
}
