package pty

import "testing"

func TestWinsizeFromInts(t *testing.T) {
	tests := []struct {
		name     string
		rows     int
		cols     int
		wantRows uint16
		wantCols uint16
		wantOK   bool
	}{
		{name: "positive", rows: 24, cols: 80, wantRows: 24, wantCols: 80, wantOK: true},
		{name: "zero rows", rows: 0, cols: 80, wantOK: false},
		{name: "zero cols", rows: 24, cols: 0, wantOK: false},
		{name: "negative rows", rows: -1, cols: 80, wantOK: false},
		{name: "negative cols", rows: 24, cols: -1, wantOK: false},
		{name: "oversized", rows: maxWinsizeDimension + 100, cols: maxWinsizeDimension + 1, wantRows: maxWinsizeDimension, wantCols: maxWinsizeDimension, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRows, gotCols, gotOK := WinsizeFromInts(tt.rows, tt.cols)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotRows != tt.wantRows || gotCols != tt.wantCols {
				t.Fatalf("size = %dx%d, want %dx%d", gotCols, gotRows, tt.wantCols, tt.wantRows)
			}
		})
	}
}
