package tools

import (
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
)

func NewToolSet(exec sandbox.Executor, channelID string) []agent.AgentTool {
	return []agent.AgentTool{
		NewBashTool(exec, channelID),
		NewReadTool(exec, channelID),
		NewWriteTool(exec, channelID),
		NewEditTool(exec, channelID),
	}
}

func toolResult(toolCallID, toolName, text string) agent.ToolMessage {
	return agent.ToolMessage{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Contents:   []agent.Content{agent.TextContent{Text: text}},
		Timestamp:  time.Now(),
	}
}

func toolError(toolCallID, toolName, text string) agent.ToolMessage {
	return agent.ToolMessage{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Contents:   []agent.Content{agent.TextContent{Text: text}},
		IsError:    true,
		Timestamp:  time.Now(),
	}
}
