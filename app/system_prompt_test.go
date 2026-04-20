package main

import (
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal/agent"
)

func TestBuildAgentSectionEncouragesExploreAndPlanDelegation(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register(agent.Definition{Name: "explore", Description: "research"})
	registry.Register(agent.Definition{Name: "plan", Description: "planning"})
	registry.Register(agent.Definition{Name: "general-purpose", Description: "execution"})

	section := buildAgentSection(registry)

	for _, want := range []string{
		"Reach for subagents proactively when a task is large, ambiguous, cross-file",
		"Prefer explore for read-only investigation and evidence gathering.",
		"Prefer plan for implementation design, step ordering, and risk analysis.",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("agent section missing %q in %q", want, section)
		}
	}
}
