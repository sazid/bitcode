package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	def := Definition{Name: "test-agent", Description: "A test agent"}
	r.Register(def)

	got, ok := r.Get("test-agent")
	if !ok {
		t.Fatal("expected to find registered agent")
	}
	if got.Name != "test-agent" {
		t.Errorf("got name %q, want %q", got.Name, "test-agent")
	}
	if got.Description != "A test agent" {
		t.Errorf("got description %q, want %q", got.Description, "A test agent")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()

	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find unregistered agent")
	}
}

func TestRegistryOverwrite(t *testing.T) {
	r := NewRegistry()

	r.Register(Definition{Name: "a", Description: "first"})
	r.Register(Definition{Name: "a", Description: "second"})

	got, _ := r.Get("a")
	if got.Description != "second" {
		t.Errorf("got description %q, want %q", got.Description, "second")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()

	r.Register(Definition{Name: "b"})
	r.Register(Definition{Name: "a"})
	r.Register(Definition{Name: "c"})

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("got %d agents, want 3", len(list))
	}

	names := make([]string, len(list))
	for i, d := range list {
		names[i] = d.Name
	}
	sort.Strings(names)

	want := []string{"a", "b", "c"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestBuiltinDefinitions(t *testing.T) {
	defs := BuiltinDefinitions()

	if len(defs) != 3 {
		t.Fatalf("got %d builtin agents, want 3", len(defs))
	}

	byName := make(map[string]Definition)
	for _, d := range defs {
		byName[d.Name] = d
	}

	for _, name := range []string{"explore", "plan", "general-purpose"} {
		d, ok := byName[name]
		if !ok {
			t.Errorf("missing builtin agent %q", name)
			continue
		}
		if d.Source != "builtin" {
			t.Errorf("agent %q source = %q, want %q", name, d.Source, "builtin")
		}
		if d.Prompt == "" {
			t.Errorf("agent %q has empty prompt", name)
		}
		if d.Description == "" {
			t.Errorf("agent %q has empty description", name)
		}
	}

	// Verify specific fields
	explore := byName["explore"]
	if explore.Model != "" {
		t.Errorf("explore model = %q, want empty so it inherits from the parent", explore.Model)
	}
	if explore.MaxTurns != 30 {
		t.Errorf("explore max_turns = %d, want 30", explore.MaxTurns)
	}
	if len(explore.Tools) != 4 {
		t.Errorf("explore tools count = %d, want 4", len(explore.Tools))
	}
	if explore.Tools[2] != "LineCount" {
		t.Errorf("explore tools = %v, expected LineCount to be included", explore.Tools)
	}
	if !strings.Contains(explore.Prompt, "read-only codebase reconnaissance") {
		t.Errorf("explore prompt = %q, expected stronger explore guidance", explore.Prompt)
	}

	plan := byName["plan"]
	if plan.MaxTurns != 50 {
		t.Errorf("plan max_turns = %d, want 50", plan.MaxTurns)
	}
	if len(plan.Tools) != 5 {
		t.Errorf("plan tools count = %d, want 5", len(plan.Tools))
	}
	if !strings.Contains(plan.Prompt, "design implementation plans before coding") {
		t.Errorf("plan prompt = %q, expected stronger plan guidance", plan.Prompt)
	}

	gp := byName["general-purpose"]
	if gp.MaxTurns != 100 {
		t.Errorf("general-purpose max_turns = %d, want 100", gp.MaxTurns)
	}
	if len(gp.Tools) != 0 {
		t.Errorf("general-purpose tools count = %d, want 0", len(gp.Tools))
	}
}

func TestLoadDefinitions(t *testing.T) {
	// Create a temp directory with a test agent definition
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".bitcode", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: test-custom
description: A custom test agent
max_turns: 10
tools: [Read, Bash]
---
You are a custom test agent.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "test-custom.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory so LoadDefinitions picks it up as project-level
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	defs := LoadDefinitions()

	var found bool
	for _, d := range defs {
		if d.Name == "test-custom" {
			found = true
			if d.Source != "project" {
				t.Errorf("source = %q, want %q", d.Source, "project")
			}
			if d.Description != "A custom test agent" {
				t.Errorf("description = %q, want %q", d.Description, "A custom test agent")
			}
			if d.MaxTurns != 10 {
				t.Errorf("max_turns = %d, want 10", d.MaxTurns)
			}
			if len(d.Tools) != 2 {
				t.Errorf("tools count = %d, want 2", len(d.Tools))
			}
			break
		}
	}

	if !found {
		t.Error("LoadDefinitions did not find test-custom agent")
	}
}
