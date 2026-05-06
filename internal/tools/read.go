package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
)

var imageExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func NewReadTool(exec sandbox.Executor, channelID string) agent.AgentTool {
	return agent.AgentTool{
		Tool: agent.Tool{
			Name:        "read",
			Description: "Read a file from the sandbox workspace. Supports text and image files.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":  map[string]any{"type": "string", "description": "Brief description"},
					"path":   map[string]any{"type": "string", "description": "File path inside the container workspace"},
					"offset": map[string]any{"type": "number", "description": "Line to start reading from (1-indexed)"},
					"limit":  map[string]any{"type": "number", "description": "Max lines to read"},
				},
				"required": []string{"label", "path"},
			},
		},
		Execute: func(toolCallID string, params map[string]any) (agent.ToolMessage, error) {
			path, _ := params["path"].(string)
			if path == "" {
				return agent.ToolMessage{}, fmt.Errorf("read: path is required")
			}

			offset := 1
			if o, ok := params["offset"].(float64); ok && o >= 1 {
				offset = int(o)
			}

			var userLimit int
			if l, ok := params["limit"].(float64); ok && l > 0 {
				userLimit = int(l)
			}

			ext := ""
			if idx := strings.LastIndex(path, "."); idx >= 0 {
				ext = strings.ToLower(path[idx:])
			}
			if mime, ok := imageExts[ext]; ok {
				result, err := exec.Exec(context.Background(), channelID, "base64 < "+shellEscape(path), sandbox.ExecOptions{})
				if err != nil {
					return agent.ToolMessage{}, fmt.Errorf("read: %w", err)
				}
				if result.Code != 0 {
					return toolError(toolCallID, "read", result.Stderr), nil
				}
				return agent.ToolMessage{
					ToolCallID: toolCallID,
					ToolName:   "read",
					Contents: []agent.Content{agent.ImageContent{
						Base64:   strings.TrimSpace(result.Stdout),
						MimeType: mime,
					}},
					Timestamp: time.Now(),
				}, nil
			}

			wcResult, err := exec.Exec(context.Background(), channelID, "wc -l < "+shellEscape(path), sandbox.ExecOptions{})
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("read: %w", err)
			}
			if wcResult.Code != 0 {
				return toolError(toolCallID, "read", wcResult.Stderr), nil
			}
			totalLines, _ := strconv.Atoi(strings.TrimSpace(wcResult.Stdout))

			if offset > totalLines+1 {
				return toolError(toolCallID, "read", fmt.Sprintf("offset %d is beyond end of file (%d lines)", offset, totalLines)), nil
			}

			var readCmd string
			if offset > 1 {
				readCmd = fmt.Sprintf("tail -n +%d %s", offset, shellEscape(path))
			} else {
				readCmd = "cat " + shellEscape(path)
			}

			readResult, err := exec.Exec(context.Background(), channelID, readCmd, sandbox.ExecOptions{})
			if err != nil {
				return agent.ToolMessage{}, fmt.Errorf("read: %w", err)
			}
			if readResult.Code != 0 {
				return toolError(toolCallID, "read", readResult.Stderr), nil
			}

			content := readResult.Stdout

			if userLimit > 0 {
				lines := strings.Split(content, "\n")
				if len(lines) > userLimit {
					lines = lines[:userLimit]
					content = strings.Join(lines, "\n")
				}
			}

			truncated, tr := TruncateHead(content)

			if tr.FirstLineExceeded {
				return toolError(toolCallID, "read", "First line exceeds 50KB. Use the bash tool to process this file."), nil
			}

			msg := truncated
			if tr.Truncated {
				nextOffset := offset + tr.OutputLines
				msg = fmt.Sprintf("%s\n[output truncated — use offset=%d to continue]", truncated, nextOffset)
			}

			return toolResult(toolCallID, "read", msg), nil
		},
	}
}
