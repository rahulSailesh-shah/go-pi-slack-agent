package tools

import (
	"context"
	"fmt"
	"path/filepath"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
)

func NewWriteTool(exec sandbox.Executor, channelID string) agent.AgentTool {
	return agent.AgentTool{
		Tool: agent.Tool{
			Name:        "write",
			Description: "Write content to a file in the sandbox workspace. Creates parent directories automatically.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":   map[string]any{"type": "string", "description": "Brief description"},
					"path":    map[string]any{"type": "string", "description": "File path inside the container workspace"},
					"content": map[string]any{"type": "string", "description": "Content to write"},
				},
				"required": []string{"label", "path", "content"},
			},
		},
		Execute: func(toolCallID string, params map[string]any) (agent.ToolMessage, error) {
			path, _ := params["path"].(string)
			content, _ := params["content"].(string)
			if path == "" {
				return agent.ToolMessage{}, fmt.Errorf("write: path is required")
			}

			dir := filepath.Dir(path)
			cmd := fmt.Sprintf("mkdir -p %s && printf %%s %s > %s",
				shellEscape(dir),
				shellEscape(content),
				shellEscape(path),
			)

			result, err := exec.Exec(context.Background(), channelID, cmd, sandbox.ExecOptions{})
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("write: %w", err)
			}
			if result.Code != 0 {
				return toolError(toolCallID, "write", result.Stderr), nil
			}

			return toolResult(toolCallID, "write", fmt.Sprintf("wrote %d bytes to %s", len(content), path)), nil
		},
	}
}
