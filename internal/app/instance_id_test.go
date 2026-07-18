package app

import "testing"

func TestNewInstanceIDCombinesStableStateAndUniqueProcessParts(t *testing.T) {
	first := newInstanceID("/tmp/amux-home-a")
	second := newInstanceID("/tmp/amux-home-a")
	other := newInstanceID("/tmp/amux-home-b")

	firstNamespace, firstOK := instanceStateNamespace(first)
	secondNamespace, secondOK := instanceStateNamespace(second)
	otherNamespace, otherOK := instanceStateNamespace(other)
	if !firstOK || !secondOK || !otherOK {
		t.Fatalf("generated IDs did not parse: %q %q %q", first, second, other)
	}
	if first == second {
		t.Fatalf("per-process IDs should differ: %q", first)
	}
	if firstNamespace != secondNamespace {
		t.Fatalf("same state root produced namespaces %q and %q", firstNamespace, secondNamespace)
	}
	if firstNamespace == otherNamespace {
		t.Fatalf("different state roots produced the same namespace %q", firstNamespace)
	}
	if !instancesShareState(first, second) {
		t.Fatal("instances from the same state root should share state")
	}
	if instancesShareState(first, other) {
		t.Fatal("instances from different state roots must stay isolated")
	}
}

func TestInstancesShareStateTreatsLegacyIDsAsMigratable(t *testing.T) {
	modern := newInstanceID("/tmp/amux-home")
	for _, legacy := range []string{
		"0123456789abcdef",
		"1700000000123456789",
		"",
	} {
		if !instancesShareState(legacy, modern) {
			t.Fatalf("legacy session ID %q should remain visible during migration", legacy)
		}
	}
	for _, unknown := range []string{
		"legacy-process-id",
		"not-hex-not-hex!",
		"-1700000000123456789",
	} {
		if instancesShareState(unknown, modern) {
			t.Fatalf("unknown owner ID %q must fail closed", unknown)
		}
	}
}

func TestInstanceStateNamespaceRejectsMalformedIDs(t *testing.T) {
	for _, id := range []string{
		"",
		"legacy-process-id",
		"0000000000000000.111111111111111",
		"0000000000000000.11111111111111111",
		"not-hex-not-hex!.1111111111111111",
	} {
		if namespace, ok := instanceStateNamespace(id); ok {
			t.Fatalf("instanceStateNamespace(%q) = %q, true; want malformed", id, namespace)
		}
	}
}
