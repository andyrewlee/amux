package tmux

import (
	"strconv"
	"strings"
)

func capturePaneSnapshotData(paneID string, opts Options) ([]byte, error) {
	// -p: output to stdout
	// -e: include escape sequences (ANSI styling)
	// -S -: start from beginning of history
	// -N: preserve trailing spaces in each captured row
	// -t: target pane by globally unique pane ID
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-e", "-N", "-S", "-", "-t", paneID)
	defer cancel()
	return cmd.Output()
}

func paneSnapshotInfoForPane(
	paneID string,
	opts Options,
	coversVisibleWindow func(string, Options) (bool, error),
	metadata func(string, Options) (paneSnapshotMetadata, error),
) (int, int, bool, error) {
	if paneID == "" {
		return 0, 0, false, nil
	}
	supported, err := coversVisibleWindow(paneID, opts)
	if err != nil {
		return 0, 0, false, err
	}
	if !supported {
		return 0, 0, false, nil
	}
	meta, err := metadata(paneID, opts)
	if err != nil {
		return 0, 0, false, err
	}
	if !meta.HasSize || meta.Cols <= 0 || meta.Rows <= 0 {
		return 0, 0, false, nil
	}
	if !meta.ModeState.HasState {
		return 0, 0, false, nil
	}
	return meta.Cols, meta.Rows, true, nil
}

func capturePaneSnapshotForPane(
	paneID string,
	opts Options,
	captureData func(string, Options) ([]byte, error),
	metadata func(string, Options) (paneSnapshotMetadata, error),
) (PaneSnapshot, error) {
	before, err := metadata(paneID, opts)
	if err != nil {
		return PaneSnapshot{}, err
	}
	if !before.HasSize || before.Cols <= 0 || before.Rows <= 0 {
		return PaneSnapshot{}, errPaneSnapshotSizeMetadata
	}
	if !before.ModeState.HasState {
		return PaneSnapshot{}, errPaneSnapshotModeState
	}
	output, err := captureData(paneID, opts)
	if err != nil {
		return PaneSnapshot{}, err
	}
	after, err := metadata(paneID, opts)
	if err != nil {
		return PaneSnapshot{}, err
	}
	if !after.HasSize || after.Cols <= 0 || after.Rows <= 0 {
		return PaneSnapshot{}, errPaneSnapshotSizeMetadata
	}
	if !after.ModeState.HasState {
		return PaneSnapshot{}, errPaneSnapshotModeState
	}
	if before != after {
		return PaneSnapshot{}, errPaneSnapshotMetadataDrift
	}
	snapshot := PaneSnapshot{
		Data:      output,
		Cols:      before.Cols,
		Rows:      before.Rows,
		CursorX:   before.CursorX,
		CursorY:   before.CursorY,
		HasCursor: before.HasCursor,
		ModeState: before.ModeState,
	}
	return snapshot, nil
}

func paneSnapshotMetadataForPane(paneID string, opts Options) (paneSnapshotMetadata, error) {
	if paneID == "" {
		return paneSnapshotMetadata{}, nil
	}
	cmd, cancel := tmuxCommand(
		opts,
		"list-panes",
		"-t",
		paneID,
		"-F",
		"#{pane_id}\t#{pane_width}\t#{pane_height}\t#{cursor_x}\t#{cursor_y}\t#{alternate_on}\t#{alternate_saved_x}\t#{alternate_saved_y}\t#{cursor_flag}\t#{origin_flag}\t#{scroll_region_upper}\t#{scroll_region_lower}",
	)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return paneSnapshotMetadata{}, err
	}
	for _, line := range parseOutputLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 12 || strings.TrimSpace(parts[0]) != paneID {
			continue
		}
		meta := paneSnapshotMetadata{}
		cols, errCols := strconv.Atoi(strings.TrimSpace(parts[1]))
		rows, errRows := strconv.Atoi(strings.TrimSpace(parts[2]))
		if errCols == nil && errRows == nil && cols > 0 && rows > 0 {
			meta.Cols = cols
			meta.Rows = rows
			meta.HasSize = true
		}
		cursorX, errCursorX := strconv.Atoi(strings.TrimSpace(parts[3]))
		cursorY, errCursorY := strconv.Atoi(strings.TrimSpace(parts[4]))
		if errCursorX == nil && errCursorY == nil {
			meta.CursorX = cursorX
			meta.CursorY = cursorY
			meta.HasCursor = true
		}
		modeState, _ := parsePaneModeState([]string{
			parts[0],
			parts[5],
			parts[6],
			parts[7],
			parts[8],
			parts[9],
			parts[10],
			parts[11],
		}, paneID)
		meta.ModeState = modeState
		return meta, nil
	}
	return paneSnapshotMetadata{}, nil
}
