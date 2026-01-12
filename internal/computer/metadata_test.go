package computer

import (
	"testing"
)

func TestComputeWorktreeID(t *testing.T) {
	tests := []struct {
		name     string
		cwd      string
		wantLen  int
		wantSame bool // if true, compare with previous test
	}{
		{
			name:    "absolute path",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
		{
			name:    "different path gives different ID",
			cwd:     "/home/user/other-project",
			wantLen: 16,
		},
		{
			name:    "same path gives same ID",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
	}

	var prevID string
	var firstID string

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeWorktreeID(tt.cwd)

			if len(got) != tt.wantLen {
				t.Errorf("ComputeWorktreeID() length = %d, want %d", len(got), tt.wantLen)
			}

			// Verify it's a valid hex string
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("ComputeWorktreeID() contains invalid hex char: %c", c)
				}
			}

			// Track IDs for comparison
			if i == 0 {
				firstID = got
			} else if i == 1 {
				// Second test should be different from first
				if got == firstID {
					t.Error("Different paths should produce different IDs")
				}
				prevID = got
			} else if i == 2 {
				// Third test (same path as first) should match first
				if got != firstID {
					t.Error("Same path should produce same ID")
				}
				// And be different from second
				if got == prevID {
					t.Error("Same path should not match different path's ID")
				}
			}
		})
	}
}

func TestComputeWorktreeID_Deterministic(t *testing.T) {
	cwd := "/home/user/my-project"

	// Call multiple times, should always return same value
	id1 := ComputeWorktreeID(cwd)
	id2 := ComputeWorktreeID(cwd)
	id3 := ComputeWorktreeID(cwd)

	if id1 != id2 || id2 != id3 {
		t.Errorf("ComputeWorktreeID should be deterministic: got %s, %s, %s", id1, id2, id3)
	}
}

func TestComputeConfigHash(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name:   "empty config",
			config: map[string]any{},
		},
		{
			name: "with snapshot",
			config: map[string]any{
				"snapshot": "snap-123",
			},
		},
		{
			name: "with volumes",
			config: map[string]any{
				"volumes": []string{"data:/data"},
			},
		},
		{
			name: "with auto-stop",
			config: map[string]any{
				"autoStopInterval": int32(30),
			},
		},
		{
			name: "full config",
			config: map[string]any{
				"volumes":          []string{"data:/data"},
				"autoStopInterval": int32(30),
				"snapshot":         "snap-123",
			},
		},
	}

	hashes := make(map[string]string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := ComputeConfigHash(tt.config)

			if hash == "" {
				t.Error("ComputeConfigHash() returned empty string")
			}

			if len(hash) != 16 {
				t.Errorf("ComputeConfigHash() length = %d, want 16", len(hash))
			}

			// Store for comparison
			hashes[tt.name] = hash
		})
	}

	// Verify different configs produce different hashes
	if hashes["empty config"] == hashes["with snapshot"] {
		t.Error("Snapshot should affect config hash")
	}

	if hashes["empty config"] == hashes["with volumes"] {
		t.Error("Volumes should affect config hash")
	}

	if hashes["empty config"] == hashes["with auto-stop"] {
		t.Error("AutoStopInterval should affect config hash")
	}
}

func TestComputeConfigHash_Deterministic(t *testing.T) {
	config := map[string]any{
		"volumes":          []string{"data:/data"},
		"autoStopInterval": int32(30),
		"snapshot":         "snap-123",
	}

	hash1 := ComputeConfigHash(config)
	hash2 := ComputeConfigHash(config)
	hash3 := ComputeConfigHash(config)

	if hash1 != hash2 || hash2 != hash3 {
		t.Errorf("ComputeConfigHash should be deterministic: got %s, %s, %s", hash1, hash2, hash3)
	}
}

func TestComputeConfigHash_VolumeOrder(t *testing.T) {
	// NOTE: Current implementation does NOT sort slice entries,
	// so different volume order produces different hashes.
	// This test documents the current behavior.
	config1 := map[string]any{
		"volumes": []string{"data:/data", "cache:/cache"},
	}

	config2 := map[string]any{
		"volumes": []string{"cache:/cache", "data:/data"},
	}

	hash1 := ComputeConfigHash(config1)
	hash2 := ComputeConfigHash(config2)

	// Currently these ARE different (slice order matters)
	// This documents the behavior - a future fix could sort slices
	if hash1 == hash2 {
		t.Log("Volume order does not affect config hash (if slices are sorted)")
	} else {
		t.Log("Volume order DOES affect config hash (slices are not sorted)")
	}
}
