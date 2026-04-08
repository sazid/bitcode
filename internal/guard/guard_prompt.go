package guard

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/sazid/bitcode/internal/skills"
)

// BuildGuardSystemPrompt constructs the expert guard agent system prompt.
// skillMgr is used to list on-demand skills available to the guard agent.
func BuildGuardSystemPrompt(cwd, toolName, input string, skillMgr *skills.Manager) string {
	skillList := buildGuardSkillList(skillMgr)

	privilegeEscalation := platformPrivilegeEscalationRule()

	return fmt.Sprintf(`You are an expert security engineer and senior sysadmin with deep cloud deployment experience (AWS, GCP, Azure, Kubernetes). You review tool calls made by an AI coding agent before they execute on the user's machine.

Your responsibilities:
- Prevent irreversible destructive operations (filesystem wipes, database drops, force-pushes to protected branches, unintended deployments to production)
- Prevent data exfiltration (curl/wget to external hosts, writing secrets to accessible paths, exposing credentials in logs)
- %s
- Allow safe, routine developer operations without friction
- When uncertain, prefer to ask the user rather than silently block or silently allow

Your evaluation process:
1. Identify the language / runtime of any code in the input
2. If a language-specific skill was auto-injected below, apply its pattern checklist
3. For non-trivial commands or scripts, invoke the "simulate" skill via the Skill tool and trace the code step by step
4. Cross-reference the simulated output against the deny/ask lists
5. Return your verdict

Context:
- Working directory: %s
- Tool being evaluated: %s
- Input (truncated to 2000 bytes): %s

%sRespond with exactly one line:
  ALLOW
  DENY: <reason>
  ASK: <reason>

Lean toward ASK for ambiguous cases. Lean toward ALLOW for standard dev workflows (build, test, lint, read files). Lean toward DENY only for clearly irreversible, high-blast-radius operations.`,
		privilegeEscalation,
		cwd,
		toolName,
		truncateBytes([]byte(input), 2000),
		skillList,
	)
}

// platformPrivilegeEscalationRule returns OS-appropriate privilege escalation guidance.
func platformPrivilegeEscalationRule() string {
	if runtime.GOOS == "windows" {
		return "Prevent privilege escalation (running as Administrator without justification via Start-Process -Verb RunAs, " +
			"modifying system directories like C:\\Windows or C:\\Program Files, " +
			"disabling Windows Defender or UAC)"
	}
	return "Prevent privilege escalation (sudo without justification, chmod 777 on critical paths, writing to /etc or /usr)"
}

// buildGuardSkillList renders on-demand skills for inclusion in the system prompt.
// Auto-invoke skills are pre-injected into the user message instead.
func buildGuardSkillList(skillMgr *skills.Manager) string {
	if skillMgr == nil {
		return ""
	}

	var onDemand []skills.Skill
	for _, s := range skillMgr.List() {
		autoInvoke, _ := s.Metadata["auto_invoke"].(bool)
		if !autoInvoke {
			onDemand = append(onDemand, s)
		}
	}

	if len(onDemand) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Available guard skills (invoke via the Skill tool when needed):\n")
	for _, s := range onDemand {
		fmt.Fprintf(&sb, "- %s", s.Name)
		if s.Description != "" {
			fmt.Fprintf(&sb, ": %s", s.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}
