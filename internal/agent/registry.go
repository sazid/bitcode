package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sazid/bitcode/internal/plugin"
)

// Definition represents a user-defined or built-in agent type.
type Definition struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Prompt      string   // markdown body = system prompt

	// LLM overrides (empty = inherit from parent)
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`

	MaxTurns int      `yaml:"max_turns"`
	Tools    []string `yaml:"tools"` // tool names to include; empty = all parent tools

	Source string // "builtin", "project", "user"
}

// Registry holds agent definitions.
type Registry struct {
	defs map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]Definition)}
}

func (r *Registry) Register(def Definition) {
	r.defs[def.Name] = def
}

func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.defs[name]
	return d, ok
}

func (r *Registry) List() []Definition {
	result := make([]Definition, 0, len(r.defs))
	for _, d := range r.defs {
		result = append(result, d)
	}
	return result
}

// LoadDefinitions discovers agent definition files from the filesystem.
// It follows the same precedence as skills: user-level < project-level,
// .agents < .claude < .bitcode within each level.
// Later definitions with the same name overwrite earlier ones.
func LoadDefinitions() []Definition {
	defs := make(map[string]Definition)

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level (lower precedence)
	if home != "" {
		for _, d := range plugin.BaseDirs {
			loadAgentDir(filepath.Join(home, d, "agents"), "user", defs)
		}
	}

	// Project-level (higher precedence)
	for _, d := range plugin.BaseDirs {
		loadAgentDir(filepath.Join(wd, d, "agents"), "project", defs)
	}

	result := make([]Definition, 0, len(defs))
	for _, d := range defs {
		result = append(result, d)
	}
	return result
}

func loadAgentDir(dir, source string, defs map[string]Definition) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		def := parseDefinition(data, entry.Name(), source)
		defs[def.Name] = def
	}
}

func parseDefinition(data []byte, filename, source string) Definition {
	raw, body := plugin.ParseFrontmatter(string(data))

	name := strings.TrimSuffix(filename, ".md")
	if v, _ := raw["name"].(string); v != "" {
		name = v
	}

	desc, _ := raw["description"].(string)
	provider, _ := raw["provider"].(string)
	baseURL, _ := raw["base_url"].(string)
	apiKey, _ := raw["api_key"].(string)
	model, _ := raw["model"].(string)

	var maxTurns int
	if v, ok := raw["max_turns"].(int); ok {
		maxTurns = v
	}

	var tools []string
	if rawTools, ok := raw["tools"].([]any); ok {
		for _, t := range rawTools {
			if s, ok := t.(string); ok {
				tools = append(tools, s)
			}
		}
	}

	return Definition{
		Name:        name,
		Description: desc,
		Prompt:      body,
		Provider:    provider,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		MaxTurns:    maxTurns,
		Tools:       tools,
		Source:       source,
	}
}
