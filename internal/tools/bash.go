package tools

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
)

func NewBashTool(exec sandbox.Executor, channelID string) agent.AgentTool {
	return agent.AgentTool{
		Tool: agent.Tool{
			Name:        "bash",
			Description: "Execute a bash command inside the sandbox container. Working directory is /workspace/<channelID>.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":   map[string]any{"type": "string", "description": "Brief description shown to user"},
					"command": map[string]any{"type": "string", "description": "Bash command to execute"},
					"timeout": map[string]any{"type": "number", "description": "Timeout in seconds"},
				},
				"required": []string{"label", "command"},
			},
		},
		Execute: func(toolCallID string, params map[string]any) (agent.ToolMessage, error) {
			command, _ := params["command"].(string)
			if command == "" {
				return agent.ToolMessage{}, fmt.Errorf("bash: command is required")
			}

			var opts sandbox.ExecOptions
			if t, ok := params["timeout"].(float64); ok && t > 0 {
				opts.Timeout = time.Duration(t * float64(time.Second))
			}

			result, err := exec.Exec(context.Background(), channelID, command, opts)
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("bash: %w", err)
			}

			combined := result.Stdout + result.Stderr
			truncated, tr := TruncateTail(combined)

			msg := truncated
			if tr.Truncated {
				logFile := fmt.Sprintf("/tmp/mom-bash-%08x.log", rand.Uint32())
				writeCmd := fmt.Sprintf("printf %%s %s > %s", shellEscape(combined), logFile)
				exec.Exec(context.Background(), channelID, writeCmd, sandbox.ExecOptions{Timeout: 10 * time.Second})
				msg = fmt.Sprintf("%s\n[output truncated — full log at %s]", truncated, logFile)
			}

			if result.Code != 0 {
				msg = fmt.Sprintf("exit code %d\n%s", result.Code, msg)
				return toolError(toolCallID, "bash", msg), nil
			}

			return toolResult(toolCallID, "bash", msg), nil
		},
	}
}
