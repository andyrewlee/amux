package cli

import (
	"flag"
	"testing"
)

func TestParseSinglePositionalWithFlagsPositionalFirst(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	project := fs.String("project", "", "")

	pos, err := parseSinglePositionalWithFlags(fs, []string{"feature-x", "--project", "/tmp/repo"})
	if err != nil {
		t.Fatalf("parseSinglePositionalWithFlags() error = %v", err)
	}
	if pos != "feature-x" {
		t.Fatalf("expected positional feature-x, got %q", pos)
	}
	if *project != "/tmp/repo" {
		t.Fatalf("expected --project=/tmp/repo, got %q", *project)
	}
}

func TestParseSinglePositionalWithFlagsFlagsFirst(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	project := fs.String("project", "", "")

	pos, err := parseSinglePositionalWithFlags(fs, []string{"--project", "/tmp/repo", "feature-x"})
	if err != nil {
		t.Fatalf("parseSinglePositionalWithFlags() error = %v", err)
	}
	if pos != "feature-x" {
		t.Fatalf("expected positional feature-x, got %q", pos)
	}
	if *project != "/tmp/repo" {
		t.Fatalf("expected --project=/tmp/repo, got %q", *project)
	}
}

func TestParseSinglePositionalWithFlagsRejectsExtraPositionalPositionalFirst(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	project := fs.String("project", "", "")

	_, err := parseSinglePositionalWithFlags(
		fs,
		[]string{"feature-x", "extra-positional", "--project", "/tmp/repo"},
	)
	if err == nil {
		t.Fatalf("expected error for extra positional argument")
	}
	if *project != "" {
		t.Fatalf("expected --project to remain unset when parse fails, got %q", *project)
	}
}

func TestParseSinglePositionalWithFlagsRejectsExtraPositionalFlagsFirst(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	project := fs.String("project", "", "")

	_, err := parseSinglePositionalWithFlags(
		fs,
		[]string{"--project", "/tmp/repo", "feature-x", "extra-positional"},
	)
	if err == nil {
		t.Fatalf("expected error for extra positional argument")
	}
	if *project != "/tmp/repo" {
		t.Fatalf("expected --project=/tmp/repo, got %q", *project)
	}
}
