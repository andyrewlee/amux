package app

import (
	"os"
	"testing"
)

// envKey is a dedicated environment variable used by these tests so we never
// collide with (or clobber) a real AMUX_* variable the test runner may rely on.
const envKey = "AMUX_ENV_HELPERS_TEST_KEY"

// withCleanEnv guarantees envKey is unset both before and after the test runs,
// regardless of which branch (set/unset) the helper under test exercises.
func withCleanEnv(t *testing.T) {
	t.Helper()
	prev, hadPrev := os.LookupEnv(envKey)
	if err := os.Unsetenv(envKey); err != nil {
		t.Fatalf("unset before: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv(envKey, prev)
			return
		}
		_ = os.Unsetenv(envKey)
	})
}

func TestSetEnvIfNonEmpty(t *testing.T) {
	tests := []struct {
		name    string
		seed    string
		seedSet bool
		value   string
		wantVal string
		wantSet bool
	}{
		{
			name:    "non-empty sets value",
			value:   "hello",
			wantVal: "hello",
			wantSet: true,
		},
		{
			name: "empty is no-op when previously unset",
			// setEnvIfNonEmpty never unsets: an empty value leaves the
			// variable absent rather than explicitly unsetting it.
			value:   "",
			wantSet: false,
		},
		{
			name:    "empty preserves existing value (no unset)",
			seed:    "keepme",
			seedSet: true,
			value:   "",
			wantVal: "keepme",
			wantSet: true,
		},
		{
			name:    "whitespace-only preserves existing value",
			seed:    "keepme",
			seedSet: true,
			value:   "   ",
			wantVal: "keepme",
			wantSet: true,
		},
		{
			name:    "value is trimmed before storing",
			value:   "  spaced  ",
			wantVal: "spaced",
			wantSet: true,
		},
		{
			name:    "non-empty overwrites existing value",
			seed:    "old",
			seedSet: true,
			value:   "new",
			wantVal: "new",
			wantSet: true,
		},
		{
			name:    "internal whitespace preserved after trim",
			value:   "  a b\tc  ",
			wantVal: "a b\tc",
			wantSet: true,
		},
		{
			name:    "newline-only preserves existing value",
			seed:    "keepme",
			seedSet: true,
			value:   "\n\t\n",
			wantVal: "keepme",
			wantSet: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCleanEnv(t)
			if tt.seedSet {
				if err := os.Setenv(envKey, tt.seed); err != nil {
					t.Fatalf("seed setenv: %v", err)
				}
			}

			setEnvIfNonEmpty(envKey, tt.value)

			got, ok := os.LookupEnv(envKey)
			if ok != tt.wantSet {
				t.Fatalf("present = %v, want %v (value=%q)", ok, tt.wantSet, got)
			}
			if tt.wantSet && got != tt.wantVal {
				t.Fatalf("value = %q, want %q", got, tt.wantVal)
			}
		})
	}
}
