package main

import (
	"testing"

	"github.com/sazid/bitcode/internal/reminder"
)

func TestShouldDelegateToExplore(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                 3,
		UserText:             "find where this behavior is implemented",
		AssistantText:        "Let me inspect the relevant files.",
		RecentToolCallChains: []string{"Read>Glob", "Read>Read"},
	}
	if !shouldDelegateToExplore(state) {
		t.Fatal("expected explore heuristic to trigger")
	}
}

func TestShouldDelegateToPlan(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                 4,
		UserText:             "help me plan a cross-file refactor",
		AssistantText:        "I should outline the approach and risks.",
		RecentToolCallChains: []string{"Read>Glob", "Edit", "Read>Read"},
	}
	if !shouldDelegateToPlan(state) {
		t.Fatal("expected plan heuristic to trigger")
	}
}

func TestShouldNotDelegatePrematurely(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                 1,
		UserText:             "fix a typo",
		AssistantText:        "I'll update it.",
		RecentToolCallChains: []string{"Read"},
	}
	if shouldDelegateToExplore(state) {
		t.Fatal("did not expect explore heuristic to trigger")
	}
	if shouldDelegateToPlan(state) {
		t.Fatal("did not expect plan heuristic to trigger")
	}
}
