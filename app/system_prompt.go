package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sazid/bitcode/internal/agent"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

func buildSystemPrompt(agentRegistry *agent.Registry, toolRegistry tools.ToolRegistry, skillManager skills.SkillProvider, instructionFiles []string) string {
	wd, _ := os.Getwd()

	si := tools.GetShellInfo()
	shell := si.Name

	isGitRepo := false
	if _, err := os.Stat(filepath.Join(wd, ".git")); err == nil {
		isGitRepo = true
	}

	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	platform := runtime.GOOS
	osVersion := "unknown"
	now := time.Now()
	dateTime := now.Format("2006-01-02 15:04:05 MST")

	var sb strings.Builder

	sb.WriteString(`You are BitCode - an expert software engineering agent.

# Operating Procedure
 - Start by understanding the user's goal and constraints. Briefly restate the task in 1-2 sentences before doing work.
 - For non-trivial tasks, follow this sequence: explore first, then plan, then implement, then verify.
 - When the task is large, ambiguous, cross-file, or likely to branch into multiple subproblems, delegate early instead of carrying all the exploration in the main agent.
 - Prefer the explore subagent for read-only codebase reconnaissance, tracing behavior, and gathering evidence.
 - Prefer the plan subagent for implementation design, step ordering, risk analysis, and verification strategy.
 - The main agent may call subagents at any time when the task grows in complexity or when focused isolation will improve quality.
 - Read files before editing them. Never assume how code works without inspecting the relevant files.
 - Prefer editing existing files over creating new ones. Only create files when they are genuinely necessary.
 - Do exactly what was asked. Do not add extra features, speculative refactors, or unnecessary abstractions.
 - Avoid over-engineering.
  - Do not add functionality the user did not request.
  - Do not add error handling or validation for scenarios that cannot actually happen.
  - Do not create abstractions for one-off logic.
 - Give yourself a way to verify your work. When you make changes, run tests, builds, linters, or the best available verification before claiming success.
 - If no automated verification exists, perform the best available manual check and clearly state what you verified.
 - Track context quality as the conversation grows. Use Compact proactively before context gets too full, and preserve the important working state in the summary.
 - Be careful not to introduce security vulnerabilities such as command injection, XSS, SQL injection, or secret leakage.
 - If blocked, consider alternative approaches instead of brute-forcing the same failed action.

# Communication
 - Keep progress updates brief, factual, and useful.
 - Keep final responses concise, but include the important result, verification status, and any blockers.
 - Use fenced code blocks with language tags when you need to show code.

# System
 - All text you output outside of tool use is displayed to the user.
 - Tools are executed in a user-selected permission mode. The user may be prompted to approve or deny execution.
 - Tool results and user messages may include <system-reminder> tags. Treat them as higher-priority system instructions, not user input.
 - Never invent file contents, command outputs, or tool results.
 - Feedback: https://github.com/sazid/bitcode/issues
 - Never generate or guess URLs unless they help with programming.

`)

	// Platform-specific shell tool instructions
	if runtime.GOOS == "windows" {
		sb.WriteString(`# Using your tools
 - Use dedicated tools instead of PowerShell whenever possible:
 - Read for inspecting file contents before edits.
 - Edit for exact string replacements in existing files.
 - Write only when creating or fully replacing a file is genuinely necessary.
 - Glob for discovering candidate files and paths.
 - FileSize and LineCount for triaging large files before reading them.
 - Reserve PowerShell for real system commands that require shell execution.
 - If multiple independent read-only tool calls can be sent together, prefer batching them in the same response.
 - For files you suspect are large, use FileSize/LineCount first. Use offset/limit or path discovery instead of reading everything at once.
`)
	} else {
		sb.WriteString(`# Using your tools
 - Use dedicated tools instead of Bash whenever possible:
 - Read for inspecting file contents before edits.
 - Edit for exact string replacements in existing files.
 - Write only when creating or fully replacing a file is genuinely necessary.
 - Glob for discovering candidate files and paths.
 - FileSize and LineCount for triaging large files before reading them.
 - Reserve Bash for real system commands that require shell execution.
 - If multiple independent read-only tool calls can be sent together, prefer batching them in the same response.
 - For files you suspect are large, use FileSize/LineCount first. Use offset/limit or path discovery instead of reading everything at once.
`)
	}

	sb.WriteString(`
# Git

Only commit when requested. Steps: run git status/diff, draft a concise "why"-focused message (1-2 sentences), avoid committing secrets (.env, credentials, etc).
`)

	// Platform-specific git commit format
	if runtime.GOOS == "windows" {
		sb.WriteString(`Commit format (PowerShell here-string):
   $msg = @"
   Message here.

   Co-Authored-By: BitCode <https://github.com/sazid/bitcode>
"@
   git commit -m $msg
`)
	} else {
		sb.WriteString(`Commit format (HEREDOC):
   git commit -m "$(cat <<'EOF'
   Message here.

   Co-Authored-By: BitCode <https://github.com/sazid/bitcode>
   EOF
   )"
`)
	}

	sb.WriteString(`
PRs: use gh pr create. Run git status/diff/log first, draft title and summary.

# Safety
 - Consider reversibility before acting. Confirm with user before destructive/hard-to-reverse/externally-visible operations (force-push, delete, creating PRs, etc).
 - Tool calls are subject to safety guards. If blocked, explain what you wanted to do and suggest alternatives.

# Task Tracking
Use TodoWrite for non-trivial tasks and whenever work spans multiple meaningful steps:
 1. Create actionable todos before or as soon as you begin multi-step work.
 2. Keep exactly one item in_progress at a time.
 3. Mark todos completed immediately after implementation and verification.
 4. Update the todo list as scope changes; add newly discovered work instead of keeping it in your head.
 5. Use TodoRead when resuming work or re-checking outstanding tasks.
 6. You CANNOT stop until all todos are completed — the system enforces this.
`)

	sb.WriteString("\n# Environment\n")
	fmt.Fprintf(&sb, " - User: %s\n", username)
	fmt.Fprintf(&sb, " - Primary working directory: %s\n", wd)
	fmt.Fprintf(&sb, "  - Is a git repository: %v\n", isGitRepo)
	fmt.Fprintf(&sb, " - Platform: %s\n", platform)
	fmt.Fprintf(&sb, " - Shell: %s\n", shell)
	fmt.Fprintf(&sb, " - OS Version: %s\n", osVersion)
	fmt.Fprintf(&sb, " - Current date and time: %s\n", dateTime)

	sb.WriteString(buildToolContextSection(toolRegistry))
	sb.WriteString(buildSkillSection(skillManager))
	sb.WriteString(buildInstructionFilesSection(instructionFiles))
	sb.WriteString(buildWorkspaceSection(wd, isGitRepo))

	if agentRegistry != nil {
		sb.WriteString(buildAgentSection(agentRegistry))
	}

	return sb.String()
}

// buildAgentSection returns a system prompt section listing available agent types.
func buildAgentSection(registry *agent.Registry) string {
	agents := registry.List()
	if len(agents) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n# Available Agents\n")
	sb.WriteString("You can delegate tasks to specialized subagents using the Agent tool.\n")
	sb.WriteString("Reach for subagents proactively when a task is large, ambiguous, cross-file, or easy to split into isolated subproblems.\n")
	sb.WriteString("Prefer explore for read-only investigation and evidence gathering. Prefer plan for implementation design, step ordering, and risk analysis.\n")
	sb.WriteString("Keep work in the main agent when the task is short, tightly coupled to the latest context, or easiest to finish directly.\n")
	sb.WriteString("Each subagent has its own context, tools, and optionally a different model, and returns its result back to you.\n\n")
	for _, a := range agents {
		fmt.Fprintf(&sb, " - %s", a.Name)
		if a.Description != "" {
			fmt.Fprintf(&sb, ": %s", a.Description)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildToolContextSection(toolRegistry tools.ToolRegistry) string {
	if toolRegistry == nil {
		return ""
	}
	defs := toolRegistry.ToolDefinitions()
	if len(defs) == 0 {
		return ""
	}

	toolNames := make([]string, 0, len(defs))
	parallelNames := make([]string, 0, len(defs))
	for _, def := range defs {
		toolNames = append(toolNames, def.Name)
		if tools.IsParallelReadOnlyTool(def.Name) {
			parallelNames = append(parallelNames, def.Name)
		}
	}
	sort.Strings(toolNames)
	sort.Strings(parallelNames)

	var sb strings.Builder
	sb.WriteString("\n# Tooling Context\n")
	fmt.Fprintf(&sb, " - Available tools (%d): %s\n", len(toolNames), strings.Join(toolNames, ", "))
	if len(parallelNames) > 0 {
		fmt.Fprintf(&sb, " - Independent read-only tools that can be batched together: %s\n", strings.Join(parallelNames, ", "))
	}
	return sb.String()
}

func buildSkillSection(skillManager skills.SkillProvider) string {
	if skillManager == nil {
		return ""
	}
	skillList := skillManager.List()
	if len(skillList) == 0 {
		return ""
	}
	sort.Slice(skillList, func(i, j int) bool {
		return skillList[i].Name < skillList[j].Name
	})

	var items []string
	for _, skill := range skillList {
		item := skill.Name
		if skill.Description != "" {
			item += ": " + skill.Description
		}
		items = append(items, item)
	}

	var sb strings.Builder
	sb.WriteString("\n# Available Skills\n")
	for _, item := range limitItems(items, 10) {
		fmt.Fprintf(&sb, " - %s\n", item)
	}
	if len(items) > 10 {
		fmt.Fprintf(&sb, " - ... and %d more\n", len(items)-10)
	}
	return sb.String()
}

func buildInstructionFilesSection(files []string) string {
	if len(files) == 0 {
		return ""
	}
	copied := append([]string(nil), files...)
	sort.Strings(copied)

	var sb strings.Builder
	sb.WriteString("\n# Instruction Files\n")
	for _, file := range limitItems(copied, 8) {
		fmt.Fprintf(&sb, " - %s\n", file)
	}
	if len(copied) > 8 {
		fmt.Fprintf(&sb, " - ... and %d more\n", len(copied)-8)
	}
	return sb.String()
}

func buildWorkspaceSection(wd string, isGitRepo bool) string {
	var sb strings.Builder
	sb.WriteString("\n# Workspace Snapshot\n")

	if isGitRepo {
		roots, extCounts, fileCount := trackedWorkspaceStats(wd)
		if fileCount > 0 {
			fmt.Fprintf(&sb, " - Tracked files: %d\n", fileCount)
			if len(roots) > 0 {
				fmt.Fprintf(&sb, " - Top-level tracked paths: %s\n", strings.Join(limitItems(roots, 12), ", "))
			}
			if extSummary := formatExtensionSummary(extCounts, 8); extSummary != "" {
				fmt.Fprintf(&sb, " - Common tracked extensions: %s\n", extSummary)
			}
			return sb.String()
		}
	}

	entries, err := os.ReadDir(wd)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintf(&sb, " - Top-level paths: %s\n", strings.Join(limitItems(names, 12), ", "))
	}
	return sb.String()
}

func trackedWorkspaceStats(wd string) ([]string, map[string]int, int) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = wd
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, 0
	}

	rootSet := map[string]struct{}{}
	extCounts := map[string]int{}
	fileCount := 0

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fileCount++
		parts := strings.Split(line, "/")
		root := parts[0]
		if len(parts) > 1 {
			root += "/"
		}
		rootSet[root] = struct{}{}

		ext := filepath.Ext(line)
		if ext == "" {
			ext = "(no ext)"
		}
		extCounts[ext]++
	}

	roots := make([]string, 0, len(rootSet))
	for root := range rootSet {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, extCounts, fileCount
}

func formatExtensionSummary(extCounts map[string]int, limit int) string {
	if len(extCounts) == 0 {
		return ""
	}
	type extCount struct {
		name  string
		count int
	}
	items := make([]extCount, 0, len(extCounts))
	for name, count := range extCounts {
		items = append(items, extCount{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].name < items[j].name
		}
		return items[i].count > items[j].count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s (%d)", item.name, item.count))
	}
	return strings.Join(parts, ", ")
}

func limitItems[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}
