package slack

import (
	"context"
	"fmt"
	"log"
	"strings"

	slacklib "github.com/slack-go/slack"
)

const MaxSlackPostBytes = 35000

// PostToDestination sends text to a Slack destination.
// destination: #channel-name, @username, user id U…, or conversation id C…/G…/D…
func (c *Client) PostToDestination(ctx context.Context, destination, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("slack_post: empty text")
	}
	if len(text) > MaxSlackPostBytes {
		return fmt.Errorf("slack_post: message too long (%d bytes, max %d)", len(text), MaxSlackPostBytes)
	}

	dest := strings.TrimSpace(destination)
	if dest == "" {
		return fmt.Errorf("slack_post: empty destination")
	}
	log.Printf("slack_post: destination=%q text_len=%d", dest, len(text))

	// `#C123…` → strip leading `#`, treat as raw ID
	stripped := strings.TrimPrefix(dest, "#")
	if stripped != dest && (strings.HasPrefix(stripped, "C") || strings.HasPrefix(stripped, "G") || strings.HasPrefix(stripped, "D")) {
		return c.postToRawID(ctx, stripped, text)
	}

	switch {
	case strings.HasPrefix(dest, "#"):
		return c.postToChannelName(ctx, strings.TrimPrefix(dest, "#"), text)
	case strings.HasPrefix(dest, "@"):
		return c.postToUserHandle(ctx, strings.TrimPrefix(dest, "@"), text)
	default:
		return c.postToRawID(ctx, dest, text)
	}
}

func (c *Client) postToChannelName(ctx context.Context, name, text string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return fmt.Errorf("slack_post: empty channel name after #")
	}
	channels, err := c.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("slack_post: list channels: %w", err)
	}
	for i := range channels {
		if strings.ToLower(strings.TrimSpace(channels[i].Name)) == name {
			return c.PostMessage(channels[i].ID, text)
		}
	}
	return fmt.Errorf("slack_post: unknown channel #%s", name)
}

func (c *Client) postToUserHandle(ctx context.Context, handle, text string) error {
	handle = strings.ToLower(strings.TrimSpace(handle))
	if handle == "" {
		return fmt.Errorf("slack_post: empty user handle after @")
	}
	users, err := c.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("slack_post: list users: %w", err)
	}
	for i := range users {
		if strings.ToLower(strings.TrimSpace(users[i].Name)) == handle {
			return c.postUserDM(ctx, users[i].ID, text)
		}
	}
	return fmt.Errorf("slack_post: unknown user @%s", handle)
}

func (c *Client) postToRawID(ctx context.Context, id, text string) error {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "U") {
		return c.postUserDM(ctx, id, text)
	}
	if strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G") || strings.HasPrefix(id, "D") {
		return c.PostMessage(id, text)
	}
	return fmt.Errorf("slack_post: unknown destination %q; use #channel, @user, U…, C…, G…, or D…", id)
}

func (c *Client) postUserDM(ctx context.Context, userID, text string) error {
	ch, _, _, err := c.api.OpenConversationContext(ctx, &slacklib.OpenConversationParameters{
		Users:    []string{userID},
		ReturnIM: true,
	})
	if err != nil {
		return fmt.Errorf("slack_post: open DM with %s: %w", userID, err)
	}
	if ch == nil || ch.ID == "" {
		return fmt.Errorf("slack_post: open DM with %s: no channel in response", userID)
	}
	return c.PostMessage(ch.ID, text)
}
