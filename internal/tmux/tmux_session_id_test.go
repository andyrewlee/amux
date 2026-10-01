package tmux

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// Coverage for "#{session_id}"-keyed targeting: sessions are killed and read
// through their server-assigned "$N" id rather than their name, so a name
// carrying the tag-row field separator — or a name dying/rebinding between
// discovery and action — cannot redirect the operation at a bystander.

// recordedCall is one tmux invocation observed through the runTmuxCmd seam.
type recordedCall struct {
	sub  string
	args []string
}

// recordTmuxCalls installs a runTmuxCmd seam that records every invocation's
// subcommand and argv, then answers via respond. The seam stands in for a
// tmux server: only the argv/response contract is exercised, but the exact
// target arguments the kill/read paths emit are what this guards.
func recordTmuxCalls(t *testing.T, respond func(sub string, args []string) ([]byte, error)) *[]recordedCall {
	t.Helper()
	var calls []recordedCall
	orig := runTmuxCmd
	runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) {
		sub := ""
		for _, arg := range cmd.Args {
			switch arg {
			case "list-sessions", "has-session", "list-panes", "kill-session", "show-options":
				sub = arg
			}
		}
		calls = append(calls, recordedCall{sub: sub, args: cmd.Args})
		return respond(sub, cmd.Args)
	}
	t.Cleanup(func() { runTmuxCmd = orig })
	return &calls
}

// targetArg extracts the value of a "-t <target>" pair from a tmux argv.
func targetArg(args []string) string {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func killTargets(calls []recordedCall) []string {
	var targets []string
	for _, c := range calls {
		if c.sub == "kill-session" {
			targets = append(targets, targetArg(c.args))
		}
	}
	return targets
}

// TestKillSessionsMatchingTags_TargetsSessionID pins the kill target shape:
// matched rows are killed by "$N" id, so a name containing '|' can no longer
// steer the kill at a session whose name happens to match a parsed prefix.
func TestKillSessionsMatchingTags_TargetsSessionID(t *testing.T) {
	skipIfNoTmux(t) // EnsureAvailable still shells `tmux -V`
	calls := recordTmuxCalls(t, func(sub string, _ []string) ([]byte, error) {
		if sub == "list-sessions" {
			// id|@amux|name rows — both match @amux=1; the first name
			// carries the field separator itself.
			return []byte("$3|1|probe|pipe\n$4|1|victim\n"), nil
		}
		return nil, nil
	})

	killed, err := KillSessionsMatchingTags(map[string]string{"@amux": "1"}, testOpts())
	if err != nil || !killed {
		t.Fatalf("KillSessionsMatchingTags = (%v, %v), want (true, nil)", killed, err)
	}
	got := killTargets(*calls)
	if len(got) != 2 || got[0] != "$3" || got[1] != "$4" {
		t.Fatalf("kill targets = %v, want [$3 $4] — kills must target session ids, not names", got)
	}
	for _, c := range *calls {
		if c.sub == "kill-session" && strings.HasPrefix(targetArg(c.args), "=") {
			t.Fatalf("kill-session used a name target %q; want $id", targetArg(c.args))
		}
	}
}

// TestKillSessionsWithPrefix_TargetsSessionID covers the prefix cleanup path:
// prefix matching still happens on the name, but the kill lands on the id.
func TestKillSessionsWithPrefix_TargetsSessionID(t *testing.T) {
	skipIfNoTmux(t)
	calls := recordTmuxCalls(t, func(sub string, _ []string) ([]byte, error) {
		if sub == "list-sessions" {
			return []byte("$3\tamux-old\n$4\tother\n"), nil
		}
		return nil, nil
	})

	if err := KillSessionsWithPrefix("amux-", testOpts()); err != nil {
		t.Fatalf("KillSessionsWithPrefix: %v", err)
	}
	got := killTargets(*calls)
	if len(got) != 1 || got[0] != "$3" {
		t.Fatalf("kill targets = %v, want [$3]", got)
	}
}

// TestKillSessionsWithPrefixMissingTag_ReadsAndKillsByID covers the legacy
// cleanup path: the per-candidate option read and the kill both address the
// session id, closing the name-death/prefix-sibling check-then-act.
func TestKillSessionsWithPrefixMissingTag_ReadsAndKillsByID(t *testing.T) {
	skipIfNoTmux(t)
	calls := recordTmuxCalls(t, func(sub string, args []string) ([]byte, error) {
		switch sub {
		case "list-sessions":
			return []byte("$3\tamux-old\n$4\tamux-new\n$5\tother\n"), nil
		case "show-options":
			if targetArg(args) == "$4" {
				return []byte("inst-xyz\n"), nil
			}
			return nil, nil // $3: tag unset
		default:
			return nil, nil
		}
	})

	if err := KillSessionsWithPrefixMissingTag("amux-", "@amux_instance", testOpts()); err != nil {
		t.Fatalf("KillSessionsWithPrefixMissingTag: %v", err)
	}
	got := killTargets(*calls)
	if len(got) != 1 || got[0] != "$3" {
		t.Fatalf("kill targets = %v, want [$3]", got)
	}
	for _, c := range *calls {
		if c.sub == "show-options" {
			if tgt := targetArg(c.args); tgt != "$3" && tgt != "$4" {
				t.Fatalf("show-options targeted %q, want a $id target", tgt)
			}
		}
	}
}

// TestSessionTagValue_ReadsThroughSessionID proves the option read resolves
// name -> id first: the show-options call must address "$9", never the bare
// (prefix-matchable) name — and a pipe in the name rides through unharmed.
func TestSessionTagValue_ReadsThroughSessionID(t *testing.T) {
	skipIfNoTmux(t)
	var showTargets []string
	recordTmuxCalls(t, func(sub string, args []string) ([]byte, error) {
		switch sub {
		case "list-sessions":
			return []byte("probe|pipe\t$9\n"), nil
		case "show-options":
			showTargets = append(showTargets, targetArg(args))
			return []byte("42\n"), nil
		default:
			return nil, errors.New("unexpected subcommand " + sub)
		}
	})

	got, err := SessionTagValue("probe|pipe", "@amux", testOpts())
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "42" {
		t.Fatalf("SessionTagValue = %q, want %q", got, "42")
	}
	if len(showTargets) != 1 || showTargets[0] != "$9" {
		t.Fatalf("show-options targets = %v, want [$9]", showTargets)
	}
}

// TestSessionTagValue_MissingSessionNeverReads covers the dead/absent case:
// no id resolves, so no show-options runs at all — the old check-then-act
// read against a bare name is gone.
func TestSessionTagValue_MissingSessionNeverReads(t *testing.T) {
	skipIfNoTmux(t)
	calls := recordTmuxCalls(t, func(sub string, _ []string) ([]byte, error) {
		if sub == "list-sessions" {
			return []byte("other\t$8\n"), nil
		}
		return nil, nil
	})

	got, err := SessionTagValue("gone", "@amux", testOpts())
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "" {
		t.Fatalf("SessionTagValue = %q, want empty for a missing session", got)
	}
	for _, c := range *calls {
		if c.sub == "show-options" {
			t.Fatal("show-options ran for a session that did not resolve to an id")
		}
	}
}

// TestKillSessionsMatchingTags_PipeNameCannotForge is the end-to-end
// regression on a real server: a session named "victim|1" used to emit a row
// that parsed as Name="victim" @amux="1", redirecting the tag-matched kill
// onto the innocent "victim" session. With id-first, name-last rows the name
// is inert data and kills land on the matching row's own "$N".
func TestKillSessionsMatchingTags_PipeNameCannotForge(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "victim", "sleep 60")
	createSession(t, opts, "victim|1", "sleep 60")

	// Untagged "victim|1" must not forge a match.
	killed, err := KillSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags: %v", err)
	}
	if killed {
		t.Fatal("untagged pipe-named session must not satisfy a tag match")
	}
	if !liveSessions(t, opts)["victim"] {
		t.Fatal("forged row must not redirect the kill onto 'victim'")
	}

	// Positive control: tagging the pipe-named session itself matches and
	// kills it — by id — while "victim" survives.
	setTag(t, opts, "victim|1", "@amux", "1")
	killed, err = KillSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags(tagged): %v", err)
	}
	if !killed {
		t.Fatal("expected the tagged pipe-named session to be killed")
	}
	live := liveSessions(t, opts)
	if live["victim|1"] {
		t.Fatal("tagged pipe-named session should be dead")
	}
	if !live["victim"] {
		t.Fatal("kill must land on the matching row's own id, not the name-prefix session")
	}
}
