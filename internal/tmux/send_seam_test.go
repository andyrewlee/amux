package tmux

import (
	"testing"
	"time"
)

// TestSendKeysAppliesEnterDelayOnlyWhenSendingEnter pins the inter-keystroke
// delay behavior via the enterSleep seam: SendKeys must pause exactly once, for
// enterDelay, between the text and the carriage return when enter is true, and
// must not pause at all when enter is false. Without this, reducing or dropping
// enterDelay (re-introducing the dropped-Enter race) would pass silently.
func TestSendKeysAppliesEnterDelayOnlyWhenSendingEnter(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)

	var slept []time.Duration
	orig := enterSleep
	enterSleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { enterSleep = orig })

	// enter=true: exactly one pause, of enterDelay, between text and CR.
	if err := SendKeys("_keepalive", "x", true, opts); err != nil {
		t.Fatalf("SendKeys(enter=true): %v", err)
	}
	if len(slept) != 1 || slept[0] != enterDelay {
		t.Fatalf("expected exactly one %v pause, got %v", enterDelay, slept)
	}

	// enter=false: no pause at all.
	slept = nil
	if err := SendKeys("_keepalive", "y", false, opts); err != nil {
		t.Fatalf("SendKeys(enter=false): %v", err)
	}
	if len(slept) != 0 {
		t.Fatalf("expected no pause when enter=false, got %v", slept)
	}
}
