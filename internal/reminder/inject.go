package reminder

import (
	"fmt"
	"strings"

	"github.com/sazid/bitcode/internal/llm"
)

// InjectReminders creates a copy of the messages slice with reminders injected
// as <system-reminder> tags appended to the last user or tool message.
//
// The original messages slice is never mutated. This ensures reminders don't
// accumulate in conversation history — each turn re-evaluates which reminders
// should fire.
func InjectReminders(messages []llm.Message, reminders []Reminder) []llm.Message {
	if len(reminders) == 0 {
		return messages
	}

	// Build the combined reminder text
	var sb strings.Builder
	for _, r := range reminders {
		fmt.Fprintf(&sb, "\n<system-reminder>\n%s\n</system-reminder>", r.Content)
	}
	reminderText := sb.String()

	// Shallow copy the messages slice
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	// Find the last user or tool message to append to
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role != llm.RoleUser && result[i].Role != llm.RoleTool {
			continue
		}

		// Deep copy this message's content blocks
		newContent := make([]llm.ContentBlock, len(result[i].Content))
		copy(newContent, result[i].Content)

		// Append reminder text to the last text block
		appended := false
		for j := len(newContent) - 1; j >= 0; j-- {
			if newContent[j].Type == llm.ContentText {
				newContent[j] = llm.ContentBlock{
					Type: llm.ContentText,
					Text: newContent[j].Text + reminderText,
				}
				appended = true
				break
			}
		}

		// If no text block found, add one
		if !appended {
			newContent = append(newContent, llm.ContentBlock{
				Type: llm.ContentText,
				Text: reminderText,
			})
		}

		result[i] = llm.Message{
			Role:       result[i].Role,
			Content:    newContent,
			ToolCalls:  result[i].ToolCalls,
			ToolCallID: result[i].ToolCallID,
		}
		break
	}

	return result
}
