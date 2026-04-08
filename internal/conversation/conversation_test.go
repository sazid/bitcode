package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal/llm"
)

func TestManagerCreateAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create conversation
	conv, err := mgr.Create("Test Conversation")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if conv.ID == "" {
		t.Error("expected non-empty ID")
	}
	if conv.Title != "Test Conversation" {
		t.Errorf("expected title 'Test Conversation', got %q", conv.Title)
	}
	if conv.MessageCount != 0 {
		t.Errorf("expected 0 messages, got %d", conv.MessageCount)
	}

	// Load conversation
	loaded, err := mgr.Load(conv.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != conv.ID {
		t.Errorf("expected ID %q, got %q", conv.ID, loaded.ID)
	}
	if loaded.Title != conv.Title {
		t.Errorf("expected title %q, got %q", conv.Title, loaded.Title)
	}
}

func TestAppendMessage(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Test")

	// Append messages
	msg1 := llm.TextMessage(llm.RoleUser, "Hello")
	msg2 := llm.TextMessage(llm.RoleAssistant, "Hi there")

	if err := mgr.AppendMessage(conv.ID, msg1); err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	if err := mgr.AppendMessage(conv.ID, msg2); err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}

	// Load and verify
	loaded, err := mgr.Load(conv.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Text() != "Hello" {
		t.Errorf("expected first message 'Hello', got %q", loaded.Messages[0].Text())
	}
	if loaded.Messages[1].Text() != "Hi there" {
		t.Errorf("expected second message 'Hi there', got %q", loaded.Messages[1].Text())
	}
}

func TestList(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create multiple conversations
	conv1, _ := mgr.Create("First")
	conv2, _ := mgr.Create("Second")

	// List
	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(list))
	}

	// Should be sorted by UpdatedAt desc (most recent first)
	if list[0].ID != conv2.ID && list[0].ID != conv1.ID {
		t.Error("unexpected conversation order")
	}
}

func TestSearch(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Test")
	mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, "Hello world"))
	mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleAssistant, "Goodbye world"))

	// Search for "hello" (case insensitive)
	results, err := mgr.Search("HELLO")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(results[0].Matches))
	}
	if results[0].Matches[0] != 0 {
		t.Errorf("expected match at index 0, got %d", results[0].Matches[0])
	}

	// Search for "world"
	results, err = mgr.Search("world")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(results[0].Matches))
	}

	// Search for non-existent
	results, err = mgr.Search("nonexistent")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFork(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create original conversation
	conv, _ := mgr.Create("Original")
	mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, "First"))
	mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleAssistant, "Second"))
	mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, "Third"))

	// Fork at index 2 (keep only "First" and "Second")
	forked, err := mgr.Fork(conv.ID, "Forked", 2)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	if forked.ID == conv.ID {
		t.Error("forked conversation should have different ID")
	}
	if forked.Title != "Forked" {
		t.Errorf("expected title 'Forked', got %q", forked.Title)
	}
	if len(forked.Messages) != 2 {
		t.Errorf("expected 2 messages in fork, got %d", len(forked.Messages))
	}
	if forked.Messages[0].Text() != "First" {
		t.Errorf("expected first message 'First', got %q", forked.Messages[0].Text())
	}
	if forked.Messages[1].Text() != "Second" {
		t.Errorf("expected second message 'Second', got %q", forked.Messages[1].Text())
	}

	// Verify original still exists
	original, err := mgr.Load(conv.ID)
	if err != nil {
		t.Fatalf("Load original: %v", err)
	}
	if len(original.Messages) != 3 {
		t.Errorf("original should still have 3 messages, got %d", len(original.Messages))
	}
}

func TestRename(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Old Title")

	if err := mgr.Rename(conv.ID, "New Title"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	loaded, _ := mgr.Load(conv.ID)
	if loaded.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", loaded.Title)
	}
}

func TestDelete(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("To Delete")

	if err := mgr.Delete(conv.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = mgr.Load(conv.ID)
	if err == nil {
		t.Error("expected error loading deleted conversation")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}
	if id1 == id2 {
		t.Error("expected different IDs")
	}
}

func TestTruncateTitle(t *testing.T) {
	short := "Short title"
	long := "This is a very long title that should be truncated because it exceeds the maximum length allowed"

	if truncateTitle(short) != short {
		t.Errorf("short title should not be truncated")
	}

	truncated := truncateTitle(long)
	if len(truncated) > 63 { // 60 + "..."
		t.Errorf("truncated title too long: %d chars", len(truncated))
	}
}

func TestDefaultDir(t *testing.T) {
	dir := DefaultDir()
	if dir == "" {
		t.Error("expected non-empty default dir")
	}
	if !filepath.IsAbs(dir) {
		t.Error("expected absolute path")
	}
	if !contains(dir, ".bitcode") {
		t.Error("expected path to contain .bitcode")
	}
	if !contains(dir, "conversations") {
		t.Error("expected path to contain conversations")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadConversationWithLargeMessages(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Large Messages")

	// Append a message with content that could trip up json.Decoder
	bigMsg := llm.TextMessage(llm.RoleAssistant, strings.Repeat("line\n", 1000))
	if err := mgr.AppendMessage(conv.ID, bigMsg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	loaded, err := mgr.Load(conv.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(loaded.Messages))
	}
	if !strings.Contains(loaded.Messages[0].Text(), "line\n") {
		t.Error("message content corrupted")
	}
}

func TestMessageCountComputedOnLoad(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Count Test")

	for i := 0; i < 5; i++ {
		mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, fmt.Sprintf("msg %d", i)))
	}

	loaded, err := mgr.Load(conv.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.MessageCount != 5 {
		t.Errorf("expected MessageCount 5, got %d", loaded.MessageCount)
	}
	if len(loaded.Messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(loaded.Messages))
	}
}

func TestConcurrentListAndAppend(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	conv, _ := mgr.Create("Concurrent Test")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			mgr.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, fmt.Sprintf("msg %d", i)))
		}
	}()

	for i := 0; i < 50; i++ {
		_, err := mgr.List()
		if err != nil {
			t.Errorf("List failed: %v", err)
		}
	}
	<-done
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
