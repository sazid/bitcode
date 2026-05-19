package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func TestNormalizeToolInput_RepairsPathAliasesMarkdownNullsAndNumbers(t *testing.T) {
	normalized, repairs, err := normalizeInputForTool("Read", `{"path":"/tmp/project/[notes.md](http://notes. md)","offset":null,"limit":"30"}`, (&ReadTool{}).ParametersSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repairs) != 4 {
		t.Fatalf("expected 4 repairs, got %d: %#v", len(repairs), repairs)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(normalized), &got); err != nil {
		t.Fatalf("normalized input is not valid JSON: %v", err)
	}
	if got["file_path"] != "/tmp/project/notes.md" {
		t.Fatalf("file_path = %#v, want markdown link unwrapped path", got["file_path"])
	}
	if _, ok := got["path"]; ok {
		t.Fatal("expected path alias to be removed")
	}
	if _, ok := got["offset"]; ok {
		t.Fatal("expected null optional offset to be omitted")
	}
	if got["limit"] != float64(30) {
		t.Fatalf("limit = %#v, want 30", got["limit"])
	}
}

func TestNormalizeToolInput_ParsesStringifiedArrayBeforeWrapping(t *testing.T) {
	normalized, repairs, err := normalizeInputForTool("WebSearch", `{"query":"bitcode","allowed_domains":"[\"example.com\",\"docs.example.com\"]","blocked_domains":"spam.example"}`, (&WebSearchTool{}).ParametersSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repairs) != 2 {
		t.Fatalf("expected 2 repairs, got %d: %#v", len(repairs), repairs)
	}

	var got WebSearchInput
	if err := json.Unmarshal([]byte(normalized), &got); err != nil {
		t.Fatalf("failed to unmarshal normalized input: %v", err)
	}
	if len(got.AllowedDomains) != 2 || got.AllowedDomains[0] != "example.com" || got.AllowedDomains[1] != "docs.example.com" {
		t.Fatalf("allowed_domains = %#v, want parsed JSON array", got.AllowedDomains)
	}
	if len(got.BlockedDomains) != 1 || got.BlockedDomains[0] != "spam.example" {
		t.Fatalf("blocked_domains = %#v, want wrapped bare string", got.BlockedDomains)
	}
}

func TestNormalizeToolInput_WrapsObjectForArraySchema(t *testing.T) {
	normalized, repairs, err := normalizeInputForTool("TodoWrite", `{"todos":{"content":"Fix bug","status":"pending"}}`, (&TodoWriteTool{}).ParametersSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repairs) != 1 {
		t.Fatalf("expected 1 repair, got %d: %#v", len(repairs), repairs)
	}

	var got todoWriteInput
	if err := json.Unmarshal([]byte(normalized), &got); err != nil {
		t.Fatalf("failed to unmarshal normalized input: %v", err)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "Fix bug" || got.Todos[0].Status != "pending" {
		t.Fatalf("todos = %#v, want single object wrapped as array item", got.Todos)
	}
}

func TestNormalizeToolInput_LeavesValidJSONStringContentUntouched(t *testing.T) {
	input := `{"file_path":"notes.json","content":"[\"a\",\"b\"]"}`
	normalized, repairs, err := normalizeInputForTool("Write", input, (&WriteTool{}).ParametersSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repairs) != 0 {
		t.Fatalf("expected no repairs, got %#v", repairs)
	}
	if normalized != input {
		t.Fatalf("valid input was changed\ngot:  %s\nwant: %s", normalized, input)
	}
}

func TestManagerExecuteTool_RejectsMissingRequiredBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing-content.txt")

	mgr := NewManager()
	mgr.Register(&WriteTool{})

	_, err := mgr.ExecuteTool("Write", fmt.Sprintf(`{"file_path":%q}`, target), make(chan internal.Event, 4))
	if err == nil {
		t.Fatal("expected missing content to fail validation")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("expected target not to be written, stat err: %v", statErr)
	}
}
