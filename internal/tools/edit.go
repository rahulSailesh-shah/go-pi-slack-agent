package tools

import (
	"context"
	"fmt"
	"strings"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
)

func NewEditTool(exec sandbox.Executor, channelID string) agent.AgentTool {
	return agent.AgentTool{
		Tool: agent.Tool{
			Name:        "edit",
			Description: "Replace an exact unique string in a file. oldText must appear exactly once.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":   map[string]any{"type": "string", "description": "Brief description"},
					"path":    map[string]any{"type": "string", "description": "File path inside the container workspace"},
					"oldText": map[string]any{"type": "string", "description": "Exact text to find (must appear exactly once)"},
					"newText": map[string]any{"type": "string", "description": "Replacement text"},
				},
				"required": []string{"label", "path", "oldText", "newText"},
			},
		},
		Execute: func(toolCallID string, params map[string]any) (agent.ToolMessage, error) {
			path, _ := params["path"].(string)
			oldText, _ := params["oldText"].(string)
			newText, _ := params["newText"].(string)
			if path == "" {
				return agent.ToolMessage{}, fmt.Errorf("edit: path is required")
			}
			if oldText == "" {
				return agent.ToolMessage{}, fmt.Errorf("edit: oldText is required")
			}

			result, err := exec.Exec(context.Background(), channelID, "cat "+shellEscape(path), sandbox.ExecOptions{})
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("edit: %w", err)
			}
			if result.Code != 0 {
				return toolError(toolCallID, "edit", result.Stderr), nil
			}

			content := result.Stdout
			count := strings.Count(content, oldText)
			switch {
			case count == 0:
				return toolError(toolCallID, "edit", fmt.Sprintf("Could not find the exact text in %s.", path)), nil
			case count > 1:
				return toolError(toolCallID, "edit", fmt.Sprintf("Found %d occurrences. The text must be unique. Please provide more context.", count)), nil
			}

			newContent := strings.Replace(content, oldText, newText, 1)
			if newContent == content {
				return toolError(toolCallID, "edit", "Replacement produced identical content."), nil
			}

			writeCmd := fmt.Sprintf("printf %%s %s > %s", shellEscape(newContent), shellEscape(path))
			writeResult, err := exec.Exec(context.Background(), channelID, writeCmd, sandbox.ExecOptions{})
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("edit: %w", err)
			}
			if writeResult.Code != 0 {
				return toolError(toolCallID, "edit", writeResult.Stderr), nil
			}

			diff := fmt.Sprintf("-%s\n+%s", oldText, newText)
			return toolResult(toolCallID, "edit", fmt.Sprintf("Edited %s\n%s", path, diff)), nil
		},
	}
}
