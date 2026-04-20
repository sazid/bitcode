package main

import "testing"

func TestParseConversationTarget(t *testing.T) {
	convID, count, err := parseConversationTarget("swift-falcon-123 8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if convID != "swift-falcon-123" {
		t.Fatalf("expected conversation id swift-falcon-123, got %q", convID)
	}
	if count != 8 {
		t.Fatalf("expected count 8, got %d", count)
	}
}

func TestParseConversationTargetWithoutCount(t *testing.T) {
	convID, count, err := parseConversationTarget("swift-falcon-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if convID != "swift-falcon-123" {
		t.Fatalf("expected conversation id swift-falcon-123, got %q", convID)
	}
	if count != -1 {
		t.Fatalf("expected default count -1, got %d", count)
	}
}

func TestParseConversationTargetRejectsInvalidCount(t *testing.T) {
	_, _, err := parseConversationTarget("swift-falcon-123 nope")
	if err == nil {
		t.Fatal("expected error for invalid count")
	}
}
