package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sazid/bitcode/internal"
)

func executeGlob(t *testing.T, input GlobInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &GlobTool{}
	ch := make(chan internal.Event, 10)
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

// buildTree creates a directory tree from a map of relative-path → content.
func buildTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestGlobTool_SimpleExtension(t *testing.T) {
	root := buildTree(t, map[string]string{
		"a.go":  "",
		"b.go":  "",
		"c.txt": "",
	})

	result, err := executeGlob(t, GlobInput{Pattern: "*.go", Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := nonEmptyLines(result.Content)
	if len(paths) != 2 {
		t.Errorf("expected 2 .go files, got %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".go") {
			t.Errorf("unexpected non-.go file: %s", p)
		}
	}
}

func TestGlobTool_DoubleStarRecursive(t *testing.T) {
	root := buildTree(t, map[string]string{
		"main.go":         "",
		"pkg/foo/foo.go":  "",
		"pkg/bar/bar.go":  "",
		"pkg/bar/bar.txt": "",
	})

	result, err := executeGlob(t, GlobInput{Pattern: "**/*.go", Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := nonEmptyLines(result.Content)
	if len(paths) != 3 {
		t.Errorf("expected 3 .go files, got %d: %v", len(paths), paths)
	}
}

func TestGlobTool_DoubleStarMatchesRootToo(t *testing.T) {
	// "**/*.go" should match files at the root level as well.
	root := buildTree(t, map[string]string{
		"root.go":       "",
		"sub/nested.go": "",
	})

	result, err := executeGlob(t, GlobInput{Pattern: "**/*.go", Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := nonEmptyLines(result.Content)
	if len(paths) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(paths), paths)
	}
}

func TestGlobTool_NoMatches(t *testing.T) {
	root := buildTree(t, map[string]string{
		"a.txt": "",
	})

	result, err := executeGlob(t, GlobInput{Pattern: "*.go", Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "" {
		t.Errorf("expected empty content for no matches, got %q", result.Content)
	}
}

func TestGlobTool_SortedByModTimeNewestFirst(t *testing.T) {
	root := t.TempDir()
	// Write files with explicit delays is flaky; instead manipulate mtimes.
	files := []string{"old.go", "mid.go", "new.go"}
	base := int64(1_000_000)
	for i, name := range files {
		p := filepath.Join(root, name)
		os.WriteFile(p, []byte(""), 0o600)
		// Set atime=mtime, incrementing so new.go is newest.
		mtime := time.Unix(0, base+int64(i)*1000)
		os.Chtimes(p, mtime, mtime)
	}

	result, err := executeGlob(t, GlobInput{Pattern: "*.go", Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := nonEmptyLines(result.Content)
	if len(paths) != 3 {
		t.Fatalf("expected 3 files, got %d", len(paths))
	}
	if !strings.HasSuffix(paths[0], "new.go") {
		t.Errorf("expected newest file first, got %s", paths[0])
	}
	if !strings.HasSuffix(paths[2], "old.go") {
		t.Errorf("expected oldest file last, got %s", paths[2])
	}
}

func TestGlobTool_DefaultsToCurrentWorkingDirectory(t *testing.T) {
	root := buildTree(t, map[string]string{"hello.go": ""})

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	os.Chdir(root)

	// No Path provided — should use cwd.
	result, err := executeGlob(t, GlobInput{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := nonEmptyLines(result.Content)
	if len(paths) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(paths), paths)
	}
}

func TestGlobTool_EmitsEvent(t *testing.T) {
	root := buildTree(t, map[string]string{"x.go": ""})
	raw, _ := json.Marshal(GlobInput{Pattern: "*.go", Path: root})

	tool := &GlobTool{}
	ch := make(chan internal.Event, 10)
	_, err := tool.Execute(raw, ch)
	close(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "Glob" {
		t.Errorf("expected event name 'Glob', got %q", events[0].Name)
	}
	if len(events[0].Args) == 0 || events[0].Args[0] != "*.go" {
		t.Errorf("expected pattern in event args, got %v", events[0].Args)
	}
}

func TestGlobTool_RelativePath(t *testing.T) {
	root := buildTree(t, map[string]string{"z.go": ""})

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	os.Chdir(filepath.Dir(root))

	result, err := executeGlob(t, GlobInput{Pattern: "*.go", Path: filepath.Base(root)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "z.go") {
		t.Errorf("expected z.go in results, got %q", result.Content)
	}
}

// helpers

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
