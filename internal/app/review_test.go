package app

import (
	"strings"
	"testing"
)

func TestBuildReviewMessageIncludesSideAndLine(t *testing.T) {
	a := &App{
		diffComments: map[string][]reviewComment{},
	}
	a.diffComments[makeCommentKey("main.go", "new", 10)] = []reviewComment{
		{Body: "Update error handling", Code: "return err", Side: "new", Line: 10},
	}
	a.diffComments[makeCommentKey("main.go", "old", 4)] = []reviewComment{
		{Body: "Remove unused var", Code: "tmp := 1", Side: "old", Line: 4},
	}

	message := a.buildReviewMessage()
	if !strings.Contains(message, "L10 new") {
		t.Fatalf("expected new side line label in message:\n%s", message)
	}
	if !strings.Contains(message, "L4 old") {
		t.Fatalf("expected old side line label in message:\n%s", message)
	}
	if !strings.Contains(message, "`return err`") || !strings.Contains(message, "`tmp := 1`") {
		t.Fatalf("expected code snippets in message:\n%s", message)
	}
}
