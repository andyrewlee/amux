package cli

import (
	"reflect"
	"testing"
)

func TestInsertFlagAfterCobraCommandPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nested command",
			args: []string{"sandbox", "ls"},
			want: []string{"sandbox", "ls", "--json"},
		},
		{
			name: "root alias",
			args: []string{"ls"},
			want: []string{"ls", "--json"},
		},
		{
			name: "passthrough command",
			args: []string{"exec", "--", "rg", "--json"},
			want: []string{"exec", "--json", "--", "rg", "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InsertFlagAfterCobraCommandPath(tt.args, "--json")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("InsertFlagAfterCobraCommandPath(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
