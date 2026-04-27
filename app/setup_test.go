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

func TestShouldNotDelegateToExploreTwice(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                  4,
		UserText:              "find where this behavior is implemented",
		AssistantText:         "Let me inspect the relevant files.",
		RecentToolCallChains:  []string{"Read>Glob", "Read>Read"},
		RecentDelegatedAgents: []string{"explore"},
	}
	if shouldDelegateToExplore(state) {
		t.Fatal("did not expect explore heuristic to retrigger after recent delegation")
	}
}

func TestShouldDelegateToExploreAfterOlderDelegation(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                  8,
		UserText:              "find where this behavior is implemented",
		AssistantText:         "Let me inspect the relevant files.",
		RecentToolCallChains:  []string{"Read>Glob", "Read>Read"},
		RecentDelegatedAgents: []string{"explore", "plan", "general-purpose", "plan"},
	}
	if !shouldDelegateToExplore(state) {
		t.Fatal("expected explore heuristic to trigger again after older delegation aged out")
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

func TestShouldNotDelegateToPlanTwice(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                  5,
		UserText:              "help me plan a cross-file refactor",
		AssistantText:         "I should outline the approach and risks.",
		RecentToolCallChains:  []string{"Read>Glob", "Edit", "Read>Read"},
		RecentDelegatedAgents: []string{"explore", "plan"},
	}
	if shouldDelegateToPlan(state) {
		t.Fatal("did not expect plan heuristic to retrigger after recent delegation")
	}
}

func TestShouldDelegateToPlanAfterOlderDelegation(t *testing.T) {
	state := &reminder.ConversationState{
		Turn:                  8,
		UserText:              "help me plan a cross-file refactor",
		AssistantText:         "I should outline the approach and risks.",
		RecentToolCallChains:  []string{"Read>Glob", "Edit", "Read>Read"},
		RecentDelegatedAgents: []string{"plan", "explore", "general-purpose", "explore"},
	}
	if !shouldDelegateToPlan(state) {
		t.Fatal("expected plan heuristic to trigger again after older delegation aged out")
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
