package update

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    Version
		wantErr bool
	}{
		{
			input: "v1.2.3",
			want:  Version{Major: 1, Minor: 2, Patch: 3, Raw: "v1.2.3"},
		},
		{
			input: "1.2.3",
			want:  Version{Major: 1, Minor: 2, Patch: 3, Raw: "1.2.3"},
		},
		{
			input: "v0.1.0",
			want:  Version{Major: 0, Minor: 1, Patch: 0, Raw: "v0.1.0"},
		},
		{
			input: "v2.0.0-beta.1",
			want:  Version{Major: 2, Minor: 0, Patch: 0, Prerelease: "beta.1", Raw: "v2.0.0-beta.1"},
		},
		{
			input: "v1.0.0-rc1",
			want:  Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc1", Raw: "v1.0.0-rc1"},
		},
		{
			input:   "invalid",
			wantErr: true,
		},
		{
			input:   "",
			wantErr: true,
		},
		{
			input:   "v1",
			wantErr: true,
		},
		{
			input:   "v1.2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.Major != tt.want.Major || got.Minor != tt.want.Minor || got.Patch != tt.want.Patch || got.Prerelease != tt.want.Prerelease {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1: a < b, 0: a == b, 1: a > b
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.1.0", -1},
		{"v1.1.0", "v1.0.0", 1},
		{"v1.0.0", "v2.0.0", -1},
		{"v2.0.0", "v1.0.0", 1},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.10", "v1.2.9", 1},
		// Prerelease versions
		{"v1.0.0-alpha", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-alpha", 1},
		{"v1.0.0-alpha", "v1.0.0-beta", -1},
		{"v1.0.0-beta", "v1.0.0-alpha", 1},
		{"v1.0.0-alpha", "v1.0.0-alpha", 0},
		// Numeric prerelease comparison (semver spec)
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},        // rc.2 < rc.10 (numeric comparison)
		{"v1.0.0-rc.10", "v1.0.0-rc.2", 1},         // rc.10 > rc.2
		{"v1.0.0-1", "v1.0.0-alpha", -1},           // numeric < non-numeric
		{"v1.0.0-alpha", "v1.0.0-1", 1},            // non-numeric > numeric
		{"v1.0.0-alpha.1", "v1.0.0-alpha.1.1", -1}, // shorter < longer when prefix equal
		{"v1.0.0-alpha.1.1", "v1.0.0-alpha.1", 1},  // longer > shorter
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			va, err := ParseVersion(tt.a)
			if err != nil {
				t.Fatalf("ParseVersion(%q) error = %v", tt.a, err)
			}
			vb, err := ParseVersion(tt.b)
			if err != nil {
				t.Fatalf("ParseVersion(%q) error = %v", tt.b, err)
			}
			got := va.Compare(vb)
			if got != tt.want {
				t.Errorf("Version(%q).Compare(%q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestVersionLessThan(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v0.9.0", "v1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_lt_"+tt.b, func(t *testing.T) {
			va, _ := ParseVersion(tt.a)
			vb, _ := ParseVersion(tt.b)
			got := va.LessThan(vb)
			if got != tt.want {
				t.Errorf("Version(%q).LessThan(%q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"dev", true},
		{"", true},
		{"none", true},
		{"unknown", true},
		{"v1.0.0", false},
		{"1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := IsDevBuild(tt.version)
			if got != tt.want {
				t.Errorf("IsDevBuild(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		version Version
		want    string
	}{
		{Version{Major: 1, Minor: 2, Patch: 3}, "v1.2.3"},
		{Version{Major: 0, Minor: 0, Patch: 1}, "v0.0.1"},
		{Version{Major: 2, Minor: 0, Patch: 0, Prerelease: "beta"}, "v2.0.0-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.version.String()
			if got != tt.want {
				t.Errorf("Version.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
