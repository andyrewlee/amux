package tmux

import (
	"errors"
	"testing"
)

func TestCapturePaneSnapshotForPane_RejectsModeMetadataErrors(t *testing.T) {
	modeErr := errors.New("unsupported format")
	snapshot, err := capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			if paneID != "%1" {
				t.Fatalf("expected pane %q, got %q", "%1", paneID)
			}
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{}, modeErr
		},
	)
	if !errors.Is(err, modeErr) {
		t.Fatalf("expected mode metadata failure to abort snapshot, got snapshot=%+v err=%v", snapshot, err)
	}

	snapshot, err = capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{Cols: 80, Rows: 24, HasSize: true}, nil
		},
	)
	if !errors.Is(err, errPaneSnapshotModeState) {
		t.Fatalf("expected missing mode metadata to reject snapshot, got snapshot=%+v err=%v", snapshot, err)
	}
}

func TestCapturePaneSnapshotForPane_PreservesPaneSizeMetadata(t *testing.T) {
	snapshot, err := capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{
				Cols:      91,
				Rows:      27,
				HasSize:   true,
				CursorX:   4,
				CursorY:   2,
				HasCursor: true,
				ModeState: PaneModeState{HasState: true},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("capturePaneSnapshotForPane: %v", err)
	}
	if snapshot.Cols != 91 || snapshot.Rows != 27 {
		t.Fatalf("expected pane snapshot size 91x27, got %dx%d", snapshot.Cols, snapshot.Rows)
	}
	if !snapshot.HasCursor || snapshot.CursorX != 4 || snapshot.CursorY != 2 {
		t.Fatalf("expected cursor metadata to be preserved, got %+v", snapshot)
	}
}

func TestCapturePaneSnapshotForPane_RejectsMissingPaneSizeMetadata(t *testing.T) {
	snapshot, err := capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{ModeState: PaneModeState{HasState: true}}, nil
		},
	)
	if !errors.Is(err, errPaneSnapshotSizeMetadata) {
		t.Fatalf("expected missing pane size metadata to reject snapshot, got snapshot=%+v err=%v", snapshot, err)
	}
}

func TestCapturePaneSnapshotForPane_PropagatesPaneSizeLookupErrors(t *testing.T) {
	sizeErr := errors.New("pane size lookup failed")
	snapshot, err := capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{}, sizeErr
		},
	)
	if !errors.Is(err, sizeErr) {
		t.Fatalf("expected pane size lookup failure to abort snapshot, got snapshot=%+v err=%v", snapshot, err)
	}
}

func TestCapturePaneSnapshotForPane_RejectsMetadataDriftDuringCapture(t *testing.T) {
	metadataCalls := 0
	snapshot, err := capturePaneSnapshotForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) ([]byte, error) {
			return []byte("snapshot frame"), nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			metadataCalls++
			meta := paneSnapshotMetadata{
				Cols:      91,
				Rows:      27,
				HasSize:   true,
				CursorX:   4,
				CursorY:   2,
				HasCursor: true,
				ModeState: PaneModeState{HasState: true},
			}
			if metadataCalls > 1 {
				meta.Rows = 28
			}
			return meta, nil
		},
	)
	if !errors.Is(err, errPaneSnapshotMetadataDrift) {
		t.Fatalf("expected metadata drift to reject snapshot, got snapshot=%+v err=%v", snapshot, err)
	}
}

func TestPaneSnapshotInfoForPane_RequiresModeMetadata(t *testing.T) {
	cols, rows, supported, err := paneSnapshotInfoForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) (bool, error) {
			return true, nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{Cols: 91, Rows: 27, HasSize: true}, nil
		},
	)
	if err != nil {
		t.Fatalf("paneSnapshotInfoForPane: %v", err)
	}
	if supported || cols != 0 || rows != 0 {
		t.Fatalf("expected missing mode metadata to mark pane snapshot info unsupported, got supported=%v size=%dx%d", supported, cols, rows)
	}
}

func TestPaneSnapshotInfoForPane_PreservesAuthoritativeSize(t *testing.T) {
	cols, rows, supported, err := paneSnapshotInfoForPane(
		"%1",
		Options{},
		func(paneID string, opts Options) (bool, error) {
			return true, nil
		},
		func(paneID string, opts Options) (paneSnapshotMetadata, error) {
			return paneSnapshotMetadata{
				Cols:      91,
				Rows:      27,
				HasSize:   true,
				ModeState: PaneModeState{HasState: true},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("paneSnapshotInfoForPane: %v", err)
	}
	if !supported || cols != 91 || rows != 27 {
		t.Fatalf("expected authoritative pane size to be preserved, got supported=%v size=%dx%d", supported, cols, rows)
	}
}
