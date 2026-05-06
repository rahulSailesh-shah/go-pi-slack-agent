package slack

import (
	"context"

	slacklib "github.com/slack-go/slack"
)

// OAuth scopes: users:read, channels:read, groups:read, im:read, mpim:read, chat:write, im:write

type UserSummary struct {
	ID          string
	Name        string
	DisplayName string
}

type ChannelSummary struct {
	ID   string
	Name string
}

func userSummaryFrom(u *slacklib.User) UserSummary {
	if u == nil {
		return UserSummary{}
	}
	display := u.Profile.DisplayName
	if display == "" {
		display = u.Profile.RealName
	}
	if display == "" {
		display = u.RealName
	}
	return UserSummary{ID: u.ID, Name: u.Name, DisplayName: display}
}

func channelSummaryFrom(ch *slacklib.Channel) ChannelSummary {
	if ch == nil {
		return ChannelSummary{}
	}
	name := ch.Name
	if name == "" {
		name = ch.User
	}
	return ChannelSummary{ID: ch.ID, Name: name}
}

func (c *Client) GetUser(ctx context.Context, userID string) (*UserSummary, error) {
	u, err := c.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		return nil, err
	}
	s := userSummaryFrom(u)
	return &s, nil
}

func (c *Client) GetChannel(ctx context.Context, channelID string) (*ChannelSummary, error) {
	ch, err := c.api.GetConversationInfoContext(ctx, &slacklib.GetConversationInfoInput{ChannelID: channelID})
	if err != nil {
		return nil, err
	}
	s := channelSummaryFrom(ch)
	return &s, nil
}

func (c *Client) ListUsers(ctx context.Context) ([]UserSummary, error) {
	members, err := c.api.GetUsersContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserSummary, 0, len(members))
	for i := range members {
		if members[i].Deleted {
			continue
		}
		out = append(out, userSummaryFrom(&members[i]))
	}
	return out, nil
}

func (c *Client) ListChannels(ctx context.Context) ([]ChannelSummary, error) {
	var out []ChannelSummary
	cursor := ""
	for {
		chans, next, err := c.api.GetConversationsContext(ctx, &slacklib.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: true,
			Limit:           200,
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
		})
		if err != nil {
			return nil, err
		}
		for i := range chans {
			out = append(out, channelSummaryFrom(&chans[i]))
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return out, nil
}

func (c *Client) BotUserID() string {
	return c.botUserID
}
