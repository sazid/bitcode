package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FormatStats renders session statistics as a human-readable string.
func FormatStats(stats *SessionStats) string {
	var sb strings.Builder

	elapsed := time.Since(stats.StartTime)

	sb.WriteString(fmt.Sprintf("\n  Session: %s (%s)\n", stats.SessionID, formatDuration(elapsed)))
	sb.WriteString("\n")

	// LLM stats
	sb.WriteString(fmt.Sprintf("  LLM Calls:      %d\n", stats.LLMCalls))
	sb.WriteString(fmt.Sprintf("  Total Latency:   %s\n", formatDuration(stats.TotalLatency)))
	sb.WriteString(fmt.Sprintf("  Input Tokens:    %s\n", formatNumber(stats.InputTokens)))
	sb.WriteString(fmt.Sprintf("  Output Tokens:   %s\n", formatNumber(stats.OutputTokens)))
	if stats.CacheRead > 0 || stats.CacheCreate > 0 {
		sb.WriteString(fmt.Sprintf("  Cache Read:      %s\n", formatNumber(stats.CacheRead)))
		sb.WriteString(fmt.Sprintf("  Cache Create:    %s\n", formatNumber(stats.CacheCreate)))
	}
	totalTokens := stats.InputTokens + stats.OutputTokens
	if totalTokens > 0 {
		sb.WriteString(fmt.Sprintf("  Total Tokens:    %s\n", formatNumber(totalTokens)))
	}

	// Tool stats
	if len(stats.ToolCalls) > 0 {
		sb.WriteString("\n  Tool Calls:\n")
		// Sort by count descending
		type toolCount struct {
			name  string
			count int
		}
		var tools []toolCount
		total := 0
		for name, count := range stats.ToolCalls {
			tools = append(tools, toolCount{name, count})
			total += count
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].count > tools[j].count
		})
		for _, tc := range tools {
			sb.WriteString(fmt.Sprintf("    %-16s %d\n", tc.name, tc.count))
		}
		sb.WriteString(fmt.Sprintf("    %-16s %d\n", "total", total))
		if stats.ToolErrors > 0 {
			sb.WriteString(fmt.Sprintf("    errors:          %d\n", stats.ToolErrors))
		}
	}

	// Guard stats
	if stats.GuardEvals > 0 {
		sb.WriteString(fmt.Sprintf("\n  Guard Evals:     %d\n", stats.GuardEvals))
		for verdict, count := range stats.GuardVerdicts {
			sb.WriteString(fmt.Sprintf("    %-16s %d\n", verdict, count))
		}
	}

	// Error count
	if stats.Errors > 0 {
		sb.WriteString(fmt.Sprintf("\n  Errors:          %d\n", stats.Errors))
	}

	return sb.String()
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}
