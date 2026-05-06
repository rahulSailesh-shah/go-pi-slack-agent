package tools

import (
	"context"
	"fmt"
	"log"

	agent "github.com/rahulSailesh-shah/go-pi-agent"
)

// SlackDestinationPoster posts to another Slack channel or user DM from the host process.
type SlackDestinationPoster interface {
	PostToDestination(ctx context.Context, destination, text string) error
}

// NewSlackPostTool posts messages via Slack Web API (not from the sandbox).
func NewSlackPostTool(poster SlackDestinationPoster) agent.AgentTool {
	return agent.AgentTool{
		Tool: agent.Tool{
			Name: "slack_post",
			Description: "Send a message to another Slack destination from this workspace. " +
				"destination can be #channel-name (public Slack channel name), @username (member login/handle), " +
				"or a raw id (C…/G…/D… conversation or U… user). " +
				"The bot may only message channels and users it can access per workspace lists.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label": map[string]any{
						"type":        "string",
						"description": "Brief description shown to the user",
					},
					"destination": map[string]any{
						"type":        "string",
						"description": "Target: #channel, @user, or C/G/D/U id",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Message body (mrkdwn)",
					},
				},
				"required": []string{"label", "destination", "text"},
			},
		},
		Execute: func(toolCallID string, params map[string]any) (agent.ToolMessage, error) {
			dest, _ := params["destination"].(string)
			text, _ := params["text"].(string)
			log.Printf("slack_post tool: call_id=%s dest=%q text_len=%d", toolCallID, dest, len(text))
			if dest == "" {
				return agent.ToolMessage{}, fmt.Errorf("slack_post: destination is required")
			}
			if poster == nil {
				log.Printf("slack_post tool: poster is nil, returning error")
				return toolError(toolCallID, "slack_post", "slack client not configured"), nil
			}
			err := poster.PostToDestination(context.Background(), dest, text)
			if err != nil {
				log.Printf("slack_post tool: PostToDestination error: %v", err)
				return toolError(toolCallID, "slack_post", err.Error()), nil
			}
			log.Printf("slack_post tool: message sent successfully to %q", dest)
			return toolResult(toolCallID, "slack_post", "message sent"), nil
		},
	}
}
