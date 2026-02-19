package activity

import "testing"

func TestIsRunningSession(t *testing.T) {
	tests := []struct {
		name    string
		info    SessionInfo
		hasInfo bool
		want    bool
	}{
		{
			name:    "no info returns true",
			info:    SessionInfo{},
			hasInfo: false,
			want:    true,
		},
		{
			name:    "empty status returns true",
			info:    SessionInfo{Status: ""},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "running status returns true",
			info:    SessionInfo{Status: "running"},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "detached status returns true",
			info:    SessionInfo{Status: "detached"},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "stopped status returns false",
			info:    SessionInfo{Status: "stopped"},
			hasInfo: true,
			want:    false,
		},
		{
			name:    "unknown status returns false",
			info:    SessionInfo{Status: "unknown"},
			hasInfo: true,
			want:    false,
		},
		{
			name:    "case insensitive Running",
			info:    SessionInfo{Status: "Running"},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "case insensitive DETACHED",
			info:    SessionInfo{Status: "DETACHED"},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "case insensitive Stopped",
			info:    SessionInfo{Status: "Stopped"},
			hasInfo: true,
			want:    false,
		},
		{
			name:    "whitespace trimmed",
			info:    SessionInfo{Status: "  running  "},
			hasInfo: true,
			want:    true,
		},
		{
			name:    "whitespace trimmed stopped",
			info:    SessionInfo{Status: " stopped "},
			hasInfo: true,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRunningSession(tt.info, tt.hasInfo)
			if got != tt.want {
				t.Errorf("IsRunningSession(%+v, %v) = %v, want %v", tt.info, tt.hasInfo, got, tt.want)
			}
		})
	}
}
