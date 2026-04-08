package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
)

const (
	doctorLogsCursorPrefix = "__AMUX_LOGS_CURSOR__:"
	doctorLogsTimePrefix   = "__AMUX_LOGS_TIME__:"
)

const doctorLogsJournalctlFollowCmd = `snapshot_file=$(mktemp) || exit $?
cleanup() {
  rm -f "$snapshot_file"
}
trap cleanup EXIT

boundary=$(date -u '+%%Y-%%m-%%d %%H:%%M:%%S.%%N UTC') || exit $?
journalctl --no-pager --show-cursor %s >"$snapshot_file" 2>/dev/null
status=$?
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
last_line=$(tail -n 1 "$snapshot_file" 2>/dev/null || true)
cursor=""
if [ "${last_line#-- cursor: }" != "$last_line" ]; then
  cursor=${last_line#-- cursor: }
fi
printf '%%s%%s\n' '%s' "$boundary"
printf '%%s%%s\n' '%s' "$cursor"
if [ -n "$cursor" ]; then
  sed '$d' "$snapshot_file"
else
  cat "$snapshot_file"
fi`

const doctorLogsDmesgFollowSnapshotScript = `snapshot_file=$(mktemp) || exit $?
cleanup() {
  rm -f "$snapshot_file"
}
trap cleanup EXIT

command -v dmesg >/dev/null 2>&1 || {
  printf '%s'
  exit 0
}

dmesg >"$snapshot_file" 2>/dev/null
status=$?

if [ "$status" -eq 1 ]; then
  printf '%s'
  exit 0
fi
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
cat "$snapshot_file"`

var (
	doctorLogsFollowInterval = time.Second
	doctorLogsFollowContext  = contextWithSignal
)

func doctorLogsJournalctlCmd(lines int, follow bool) string {
	if follow {
		return fmt.Sprintf(doctorLogsJournalctlFollowCmd, fmt.Sprintf("-n %d", lines), doctorLogsTimePrefix, doctorLogsCursorPrefix)
	}
	return fmt.Sprintf("journalctl --no-pager -n %d", lines)
}

func doctorLogsJournalctlAfterCursorCmd(cursor string) string {
	return fmt.Sprintf(doctorLogsJournalctlFollowCmd, "--after-cursor="+strconv.Quote(cursor), doctorLogsTimePrefix, doctorLogsCursorPrefix)
}

func doctorLogsJournalctlSinceCmd(since string) string {
	return fmt.Sprintf(doctorLogsJournalctlFollowCmd, "--since="+strconv.Quote(since), doctorLogsTimePrefix, doctorLogsCursorPrefix)
}

func doctorLogsDmesgCmd(lines int, follow bool) string {
	return fmt.Sprintf(doctorLogsDmesgSnapshotCmd, doctorLogsUnavailableMarker, doctorLogsUnavailableMarker, lines)
}

func doctorLogsDmesgFollowSnapshotCmd() string {
	return fmt.Sprintf(doctorLogsDmesgFollowSnapshotScript, doctorLogsUnavailableMarker, doctorLogsUnavailableMarker)
}

func doctorLogsSplitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func doctorLogsTail(s string, lines int) string {
	if lines <= 0 {
		return ""
	}
	all := doctorLogsSplitLines(s)
	if len(all) <= lines {
		return s
	}
	return strings.Join(all[len(all)-lines:], "")
}

type doctorLogsJournalFollowSnapshot struct {
	Resp    *sandbox.ExecResult
	Content string
	Cursor  string
	Since   string
}

func doctorLogsNormalizeJournalFollowContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "-- No entries --", "No journal files were found.":
			continue
		default:
			return content
		}
	}
	return ""
}

func doctorLogsParseJournalFollowSnapshot(resp *sandbox.ExecResult) (*doctorLogsJournalFollowSnapshot, error) {
	if resp == nil {
		return nil, errors.New("could not retrieve logs")
	}
	if resp.ExitCode != 0 {
		return &doctorLogsJournalFollowSnapshot{Resp: resp}, nil
	}

	timeHeader, remainder, found := strings.Cut(resp.Stdout, "\n")
	if !found {
		timeHeader = resp.Stdout
		remainder = ""
	}
	if !strings.HasPrefix(timeHeader, doctorLogsTimePrefix) {
		return nil, errors.New("could not retrieve logs")
	}
	cursorHeader, content, found := strings.Cut(remainder, "\n")
	if !found {
		cursorHeader = remainder
		content = ""
	}
	if !strings.HasPrefix(cursorHeader, doctorLogsCursorPrefix) {
		return nil, errors.New("could not retrieve logs")
	}
	content = doctorLogsNormalizeJournalFollowContent(content)

	normalized := *resp
	normalized.Stdout = content
	return &doctorLogsJournalFollowSnapshot{
		Resp:    &normalized,
		Content: content,
		Cursor:  strings.TrimSpace(strings.TrimPrefix(cursorHeader, doctorLogsCursorPrefix)),
		Since:   strings.TrimSpace(strings.TrimPrefix(timeHeader, doctorLogsTimePrefix)),
	}, nil
}

func doctorLogsFetchJournalFollowSnapshot(ctx context.Context, sb sandbox.RemoteSandbox, cmd string) (*doctorLogsJournalFollowSnapshot, error) {
	resp, err := sb.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve logs: %w", err)
	}
	if resp == nil {
		return nil, errors.New("could not retrieve logs")
	}
	if resp.ExitCode != 0 {
		return &doctorLogsJournalFollowSnapshot{Resp: resp}, nil
	}
	return doctorLogsParseJournalFollowSnapshot(resp)
}

func doctorLogsFetchDmesgFollowSnapshot(ctx context.Context, sb sandbox.RemoteSandbox) (*sandbox.ExecResult, bool, error) {
	resp, err := sb.Exec(ctx, doctorLogsDmesgFollowSnapshotCmd(), nil)
	if err != nil {
		return nil, false, fmt.Errorf("could not retrieve logs: %w", err)
	}
	if resp == nil {
		return nil, false, errors.New("could not retrieve logs")
	}
	resp, unavailable := doctorLogsNormalizeUnavailable(resp)
	return resp, unavailable, nil
}

func doctorLogsTrimOverlap(previous, current string) string {
	if previous == "" || current == "" {
		return current
	}
	prevLines := doctorLogsSplitLines(previous)
	currLines := doctorLogsSplitLines(current)
	maxOverlap := len(prevLines)
	if len(currLines) < maxOverlap {
		maxOverlap = len(currLines)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		match := true
		for i := 0; i < overlap; i++ {
			if prevLines[len(prevLines)-overlap+i] != currLines[i] {
				match = false
				break
			}
		}
		if match {
			return strings.Join(currLines[overlap:], "")
		}
	}
	return current
}

func doctorLogsTrimSinceReplay(previous, current string) string {
	trimmed := doctorLogsTrimOverlap(previous, current)
	if trimmed != current || previous == "" || current == "" {
		return trimmed
	}

	prevLines := doctorLogsSplitLines(previous)
	currLines := doctorLogsSplitLines(current)
	if len(prevLines) == 0 || len(currLines) <= len(prevLines) {
		return current
	}

	for start := 1; start+len(prevLines) <= len(currLines); start++ {
		match := true
		for i := range prevLines {
			if currLines[start+i] != prevLines[i] {
				match = false
				break
			}
		}
		if match {
			return strings.Join(currLines[start+len(prevLines):], "")
		}
	}
	return current
}

func doctorLogsSleepWithContext(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return exitError{code: 130}
	case <-timer.C:
		return nil
	}
}

func doctorLogsFollowJournal(ctx context.Context, sb sandbox.RemoteSandbox, lines int, interval time.Duration, initial *doctorLogsJournalFollowSnapshot, initialSince string) error {
	if initial.Content != "" {
		fmt.Fprint(cliStdout, initial.Content)
	}

	cursor := initial.Cursor
	since := initialSince
	previousContent := ""
	if cursor == "" {
		previousContent = initial.Content
	}

	for {
		if err := ctx.Err(); err != nil {
			return exitError{code: 130}
		}
		if err := doctorLogsSleepWithContext(ctx, interval); err != nil {
			return err
		}

		usingSince := cursor == ""
		cmd := doctorLogsJournalctlAfterCursorCmd(cursor)
		if usingSince {
			cmd = doctorLogsJournalctlSinceCmd(since)
		}

		snapshot, err := doctorLogsFetchJournalFollowSnapshot(ctx, sb, cmd)
		if err != nil {
			return err
		}
		resp := snapshot.Resp
		if resp.ExitCode != 0 {
			if resp.Stdout != "" {
				fmt.Fprint(cliStdout, resp.Stdout)
			}
			return exitError{code: resp.ExitCode}
		}
		if snapshot.Content != "" {
			content := snapshot.Content
			if usingSince {
				content = doctorLogsTrimSinceReplay(previousContent, content)
			}
			if content != "" {
				fmt.Fprint(cliStdout, content)
			}
		}
		if snapshot.Cursor != "" {
			cursor = snapshot.Cursor
			since = ""
			previousContent = ""
			continue
		}
		if usingSince {
			since = snapshot.Since
			previousContent = snapshot.Content
		}
	}
}

func doctorLogsFollowDmesg(ctx context.Context, sb sandbox.RemoteSandbox, lines int, interval time.Duration) error {
	initial, unavailable, err := doctorLogsFetchDmesgFollowSnapshot(ctx, sb)
	if err != nil {
		return err
	}
	if unavailable {
		fmt.Fprintln(cliStdout, "No logs available")
		return nil
	}
	if initial.ExitCode != 0 {
		if initial.Stdout != "" {
			fmt.Fprint(cliStdout, initial.Stdout)
		}
		return exitError{code: initial.ExitCode}
	}
	initialContent := initial.Stdout
	if initialContent != "" {
		fmt.Fprint(cliStdout, doctorLogsTail(initialContent, lines))
	}
	previousContent := initialContent
	for {
		if err := ctx.Err(); err != nil {
			return exitError{code: 130}
		}
		if err := doctorLogsSleepWithContext(ctx, interval); err != nil {
			return err
		}

		snapshot, unavailable, err := doctorLogsFetchDmesgFollowSnapshot(ctx, sb)
		if err != nil {
			return err
		}
		if unavailable {
			return nil
		}
		if snapshot.ExitCode != 0 {
			if snapshot.Stdout != "" {
				fmt.Fprint(cliStdout, snapshot.Stdout)
			}
			return exitError{code: snapshot.ExitCode}
		}
		if snapshot.Stdout != "" {
			fmt.Fprint(cliStdout, doctorLogsTrimOverlap(previousContent, snapshot.Stdout))
		}
		previousContent = snapshot.Stdout
	}
}

func doctorLogsFollow(ctx context.Context, sb sandbox.RemoteSandbox, lines int, interval time.Duration) error {
	journal, err := doctorLogsFetchJournalFollowSnapshot(ctx, sb, doctorLogsJournalctlCmd(lines, true))
	if err != nil {
		return err
	}
	if journal.Resp.ExitCode == 0 {
		return doctorLogsFollowJournal(ctx, sb, lines, interval, journal, journal.Since)
	}
	if !doctorLogsIsJournalUnavailableFailure(journal.Resp) {
		return doctorLogsPrintResponse(journal.Resp)
	}
	return doctorLogsFollowDmesg(ctx, sb, lines, interval)
}
