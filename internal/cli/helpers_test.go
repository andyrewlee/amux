package cli

import (
	"reflect"
	"testing"
)

func TestGetAgentArgsReturnsPassthroughArgs(t *testing.T) {
	args := []string{"claude", "--model", "o3"}

	got := getAgentArgs(args, 1)
	want := []string{"--model", "o3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getAgentArgs() = %v, want %v", got, want)
	}
}

func TestGetAgentArgsNoDashReturnsNil(t *testing.T) {
	args := []string{"claude", "--model", "o3"}

	got := getAgentArgs(args, -1)
	if got != nil {
		t.Fatalf("getAgentArgs() = %v, want nil", got)
	}
}

func TestGetAgentArgsDashWithoutValuesReturnsNil(t *testing.T) {
	args := []string{"claude"}

	got := getAgentArgs(args, 1)
	if got != nil {
		t.Fatalf("getAgentArgs() = %v, want nil", got)
	}
}
