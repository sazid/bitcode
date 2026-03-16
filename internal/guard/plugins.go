package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var pluginDirs = []string{".agents", ".claude", ".bitcode"}

// pluginFrontmatter represents the structure of a guard plugin file.
type pluginFrontmatter struct {
	ID       string        `yaml:"id"`
	Tool     string        `yaml:"tool"`
	Patterns []pluginMatch `yaml:"patterns"`
}

type pluginMatch struct {
	Match     string `yaml:"match"`      // regex for Bash command
	FileMatch string `yaml:"file_match"` // glob for file tools
	Verdict   string `yaml:"verdict"`
	Reason    string `yaml:"reason"`
}

// PluginRule is a guard rule loaded from a plugin file.
type PluginRule struct {
	id       string
	tools    []string
	patterns []compiledPattern
}

type compiledPattern struct {
	matchRe  *regexp.Regexp // for Bash command matching
	fileGlob string         // for file tool matching
	verdict  Verdict
	reason   string
}

func (r *PluginRule) Evaluate(ctx *EvalContext) *Decision {
	// Check if this rule applies to the current tool
	if len(r.tools) > 0 {
		found := false
		for _, t := range r.tools {
			if strings.EqualFold(t, ctx.ToolName) {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	for _, p := range r.patterns {
		if p.matchRe != nil && ctx.ToolName == "Bash" {
			cmd := extractCommand(ctx)
			if p.matchRe.MatchString(cmd) {
				return &Decision{Verdict: p.verdict, Reason: p.reason}
			}
		}
		if p.fileGlob != "" && (ctx.ToolName == "Write" || ctx.ToolName == "Edit" || ctx.ToolName == "Read") {
			var input map[string]any
			if err := json.Unmarshal(ctx.Input, &input); err != nil {
				continue
			}
			path, _ := input["file_path"].(string)
			if path == "" {
				continue
			}
			base := filepath.Base(path)
			if matched, _ := filepath.Match(p.fileGlob, base); matched {
				return &Decision{Verdict: p.verdict, Reason: p.reason}
			}
		}
	}

	return nil
}

// LoadPlugins scans guard plugin directories and returns parsed rules.
func LoadPlugins() []Rule {
	seen := make(map[string]*PluginRule)

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level (lower precedence)
	if home != "" {
		for _, d := range pluginDirs {
			loadGuardPluginDir(filepath.Join(home, d, "guards"), seen)
		}
	}

	// Project-level (higher precedence)
	for _, d := range pluginDirs {
		loadGuardPluginDir(filepath.Join(wd, d, "guards"), seen)
	}

	rules := make([]Rule, 0, len(seen))
	for _, r := range seen {
		rules = append(rules, r)
	}
	return rules
}

func loadGuardPluginDir(dir string, seen map[string]*PluginRule) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".md" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		rule, ok := parseGuardPlugin(string(data), ext, name)
		if !ok {
			continue
		}

		seen[rule.id] = rule
	}
}

func parseGuardPlugin(content, ext, filename string) (*PluginRule, bool) {
	var fm pluginFrontmatter

	if ext == ".yaml" || ext == ".yml" {
		if err := yaml.Unmarshal([]byte(content), &fm); err != nil {
			return nil, false
		}
	} else {
		// Markdown with frontmatter
		fm = parseGuardMarkdownFrontmatter(content)
	}

	if fm.ID == "" {
		fm.ID = strings.TrimSuffix(filename, ext)
	}

	if len(fm.Patterns) == 0 {
		return nil, false
	}

	var tools []string
	if fm.Tool != "" {
		for _, t := range strings.Split(fm.Tool, ",") {
			tools = append(tools, strings.TrimSpace(t))
		}
	}

	var compiled []compiledPattern
	for _, p := range fm.Patterns {
		cp := compiledPattern{
			fileGlob: p.FileMatch,
			verdict:  Verdict(p.Verdict),
			reason:   p.Reason,
		}
		if p.Match != "" {
			re, err := regexp.Compile(p.Match)
			if err != nil {
				continue
			}
			cp.matchRe = re
		}
		compiled = append(compiled, cp)
	}

	if len(compiled) == 0 {
		return nil, false
	}

	return &PluginRule{
		id:       fm.ID,
		tools:    tools,
		patterns: compiled,
	}, true
}

func parseGuardMarkdownFrontmatter(content string) pluginFrontmatter {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return pluginFrontmatter{}
	}

	rest := content[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return pluginFrontmatter{}
	}

	yamlBlock := rest[:idx]
	var fm pluginFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return pluginFrontmatter{}
	}

	// Parse patterns from the body if not in frontmatter
	if len(fm.Patterns) == 0 {
		body := rest[idx+4:]
		if err := yaml.Unmarshal([]byte(body), &struct {
			Patterns *[]pluginMatch `yaml:"patterns"`
		}{Patterns: &fm.Patterns}); err != nil {
			// Patterns might be in frontmatter already
		}
	}

	return fm
}
