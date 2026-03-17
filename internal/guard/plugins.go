package guard

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sazid/bitcode/internal/plugin"
	"gopkg.in/yaml.v3"
)

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

	for _, raw := range plugin.LoadFiles("guards") {
		rule, ok := convertRawToGuardRule(raw)
		if !ok {
			continue
		}
		seen[rule.id] = rule
	}

	rules := make([]Rule, 0, len(seen))
	for _, r := range seen {
		rules = append(rules, r)
	}
	return rules
}

func convertRawToGuardRule(raw plugin.RawPlugin) (*PluginRule, bool) {
	var fm pluginFrontmatter

	if raw.Metadata != nil {
		// Convert generic metadata map to typed struct via YAML roundtrip
		yamlBytes, err := yaml.Marshal(raw.Metadata)
		if err == nil {
			yaml.Unmarshal(yamlBytes, &fm)
		}
	}

	// Parse patterns from body if not found in frontmatter (MD files only)
	if len(fm.Patterns) == 0 && raw.Body != "" {
		yaml.Unmarshal([]byte(raw.Body), &struct {
			Patterns *[]pluginMatch `yaml:"patterns"`
		}{Patterns: &fm.Patterns})
	}

	if fm.ID == "" {
		fm.ID = raw.ID
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
