package process

import "testing"

func TestNextRunSuffix(t *testing.T) {
	base := "amux-ws-x-run"
	for _, tt := range []struct {
		names []string
		want  int
	}{
		{[]string{base}, 2},
		{[]string{base, base + "-2"}, 3},
		{[]string{base, base + "-3"}, 2}, // gap reused — the bug fix
		{[]string{base + "-2"}, 3},       // base dead, suffix alive
		{[]string{base, base + "-2", base + "-3"}, 4},
		{[]string{
			base + "-2", base + "-3", base + "-4", base + "-5", base + "-6",
			base + "-7", base + "-8", base + "-9", base + "-10",
		}, 11}, // double digit
		{[]string{"other-session", base + "-abc", base + "-0", base + "--1"}, 2},
	} {
		if got := nextRunSuffix(base, tt.names); got != tt.want {
			t.Errorf("nextRunSuffix(%v) = %d, want %d", tt.names, got, tt.want)
		}
	}
}
