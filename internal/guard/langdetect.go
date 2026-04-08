package guard

import (
	"encoding/json"
	"strings"

	"github.com/sazid/bitcode/internal/skills"
)

// DetectLanguage identifies the programming language/runtime from an EvalContext.
// Returns an empty string if no specific language is detected.
func DetectLanguage(evalCtx *EvalContext) string {
	switch evalCtx.ToolName {
	case "Bash":
		return detectBashLanguage(evalCtx.Input)
	case "PowerShell":
		return detectPowerShellLanguage(evalCtx.Input)
	case "Write", "Edit":
		return detectFileLanguage(evalCtx.Input)
	}
	return ""
}

// detectBashLanguage identifies the language from a Bash command string.
func detectBashLanguage(input json.RawMessage) string {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "bash"
	}
	cmd := strings.TrimSpace(params.Command)

	// Python
	if hasAnyPrefix(cmd, "python ", "python3 ", "uv run ", "uv run python", "python -c", "python3 -c") {
		return "python"
	}

	// Go
	if hasAnyPrefix(cmd, "go run ", "go build ", "go test ", "go generate") {
		return "go"
	}

	// JavaScript / TypeScript
	if hasAnyPrefix(cmd, "node ", "node -e", "deno ", "bun ", "npx ", "ts-node ", "tsx ") {
		return "js"
	}

	// Default: it's a Bash command
	return "bash"
}

// detectPowerShellLanguage identifies the language from a PowerShell command string.
func detectPowerShellLanguage(input json.RawMessage) string {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "powershell"
	}
	cmd := strings.TrimSpace(params.Command)

	// Python
	if hasAnyPrefix(cmd, "python ", "python3 ", "uv run ", "uv run python", "python -c", "python3 -c") {
		return "python"
	}

	// Go
	if hasAnyPrefix(cmd, "go run ", "go build ", "go test ", "go generate") {
		return "go"
	}

	// JavaScript / TypeScript
	if hasAnyPrefix(cmd, "node ", "node -e", "deno ", "bun ", "npx ", "ts-node ", "tsx ") {
		return "js"
	}

	// Default: it's a PowerShell command
	return "powershell"
}

// detectFileLanguage identifies the language from a file path in Write/Edit input.
func detectFileLanguage(input json.RawMessage) string {
	var params struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ""
	}

	path := params.FilePath
	if path == "" {
		path = params.Path
	}

	switch {
	case hasSuffix(path, ".py"):
		return "python"
	case hasSuffix(path, ".go"):
		return "go"
	case hasSuffix(path, ".js", ".ts", ".mjs", ".cjs", ".jsx", ".tsx"):
		return "js"
	case hasSuffix(path, ".sh", ".bash"):
		return "bash"
	}
	return ""
}

// SkillsForLanguage returns skills that should be auto-injected for the given language.
// A skill is auto-injected when its metadata contains auto_invoke: true and either
// language matches or language is empty (applies to all).
func SkillsForLanguage(mgr *skills.Manager, lang string) []skills.Skill {
	if lang == "" || mgr == nil {
		return nil
	}
	var result []skills.Skill
	for _, s := range mgr.List() {
		autoInvoke, _ := s.Metadata["auto_invoke"].(bool)
		if !autoInvoke {
			continue
		}
		skillLang, _ := s.Metadata["language"].(string)
		if skillLang == "" || skillLang == lang {
			result = append(result, s)
		}
	}
	return result
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func hasSuffix(s string, suffixes ...string) bool {
	lower := strings.ToLower(s)
	for _, suf := range suffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}
