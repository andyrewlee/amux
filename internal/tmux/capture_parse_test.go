package tmux

import "testing"

// These tests exercise the pure parsers split out of paneCursorPosition and
// paneSize. The malformed/zero-or-negative error branches guard against tmux
// emitting non-numeric or non-positive fields; a live tmux server never
// produces such output, so before the split these branches were unreachable
// from any integration test and silently uncovered. Driving the parsers with
// crafted lines lets us pin the field-count, paneID-match, strconv and
// validation behavior without a subprocess.

func TestParsePaneCursor(t *testing.T) {
	const paneID = "%3"
	tests := []struct {
		name    string
		lines   []string
		wantX   int
		wantY   int
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "matching paneID returns cursor",
			lines:  []string{"%3\t12\t7"},
			wantX:  12,
			wantY:  7,
			wantOK: true,
		},
		{
			name:   "non-matching paneID skipped then matched",
			lines:  []string{"%2\t99\t99", "%3\t4\t5"},
			wantX:  4,
			wantY:  5,
			wantOK: true,
		},
		{
			name:   "only non-matching paneIDs -> ok=false",
			lines:  []string{"%2\t99\t99"},
			wantOK: false,
		},
		{
			name:   "zero cursor is valid",
			lines:  []string{"%3\t0\t0"},
			wantX:  0,
			wantY:  0,
			wantOK: true,
		},
		{
			name:    "non-numeric cursor_x -> error",
			lines:   []string{"%3\tabc\t5"},
			wantErr: true,
		},
		{
			name:    "non-numeric cursor_y -> error",
			lines:   []string{"%3\t4\txyz"},
			wantErr: true,
		},
		{
			name:   "too few fields skipped -> ok=false",
			lines:  []string{"%3\t4"},
			wantOK: false,
		},
		{
			name:   "empty output -> ok=false",
			lines:  nil,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, ok, err := parsePaneCursor(tt.lines, paneID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (x=%d y=%d ok=%v)", x, y, ok)
				}
				if ok {
					t.Fatalf("expected ok=false on error, got ok=true")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && (x != tt.wantX || y != tt.wantY) {
				t.Fatalf("got (%d,%d), want (%d,%d)", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestParsePaneSize(t *testing.T) {
	const paneID = "%3"
	tests := []struct {
		name     string
		lines    []string
		wantCols int
		wantRows int
		wantOK   bool
		wantErr  bool
	}{
		{
			name:     "matching paneID returns size",
			lines:    []string{"%3\t80\t24"},
			wantCols: 80,
			wantRows: 24,
			wantOK:   true,
		},
		{
			name:     "non-matching paneID skipped then matched",
			lines:    []string{"%2\t1\t1", "%3\t100\t40"},
			wantCols: 100,
			wantRows: 40,
			wantOK:   true,
		},
		{
			name:   "only non-matching paneIDs -> ok=false",
			lines:  []string{"%2\t80\t24"},
			wantOK: false,
		},
		{
			name:    "non-numeric width -> error",
			lines:   []string{"%3\tabc\t24"},
			wantErr: true,
		},
		{
			name:    "non-numeric height -> error",
			lines:   []string{"%3\t80\txyz"},
			wantErr: true,
		},
		{
			name:    "zero width -> error",
			lines:   []string{"%3\t0\t24"},
			wantErr: true,
		},
		{
			name:    "zero height -> error",
			lines:   []string{"%3\t80\t0"},
			wantErr: true,
		},
		{
			name:    "negative width -> error",
			lines:   []string{"%3\t-5\t24"},
			wantErr: true,
		},
		{
			name:    "negative height -> error",
			lines:   []string{"%3\t80\t-1"},
			wantErr: true,
		},
		{
			name:   "too few fields skipped -> ok=false",
			lines:  []string{"%3\t80"},
			wantOK: false,
		},
		{
			name:   "empty output -> ok=false",
			lines:  nil,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows, ok, err := parsePaneSize(tt.lines, paneID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (cols=%d rows=%d ok=%v)", cols, rows, ok)
				}
				if ok {
					t.Fatalf("expected ok=false on error, got ok=true")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && (cols != tt.wantCols || rows != tt.wantRows) {
				t.Fatalf("got (%d,%d), want (%d,%d)", cols, rows, tt.wantCols, tt.wantRows)
			}
		})
	}
}
