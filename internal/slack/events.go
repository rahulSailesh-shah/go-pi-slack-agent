package slack

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	msglog "slack-agent/internal/store"

	"github.com/slack-go/slack/slackevents"
)

type rawFile struct {
	Name               string
	URLPrivate         string
	URLPrivateDownload string
	Mimetype           string
}

func toMessage(channelID, userID, ts, text string) msglog.Message {
	tsFloat, _ := strconv.ParseFloat(ts, 64)
	return msglog.Message{
		ID:        ts,
		ChannelID: channelID,
		Timestamp: time.Unix(int64(tsFloat), 0),
		UserID:    userID,
		Text:      stripBotMention(text),
		Platform:  "slack",
	}
}

func toFiles(files []rawFile) []msglog.File {
	result := make([]msglog.File, 0, len(files))
	for _, f := range files {
		url := f.URLPrivateDownload
		if url == "" {
			url = f.URLPrivate
		}
		if url == "" || f.Name == "" {
			continue
		}
		result = append(result, msglog.File{
			Name:        f.Name,
			URL:         url,
			ContentType: f.Mimetype,
		})
	}
	return result
}

var botMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

func stripBotMention(text string) string {
	return strings.TrimSpace(botMentionRe.ReplaceAllString(text, ""))
}

func messageEventText(ev *slackevents.MessageEvent) string {
	if ev.Text != "" {
		return ev.Text
	}
	if ev.SubType == "file_share" && ev.Message != nil && ev.Message.Text != "" {
		return ev.Message.Text
	}
	return ""
}

func mentionsBot(botUserID, text string) bool {
	return strings.Contains(text, "<@"+botUserID+">")
}

func shouldTrackMessage(ev *slackevents.MessageEvent, botUserID string) bool {
	if ev.ChannelType == "im" {
		return true
	}
	return mentionsBot(botUserID, messageEventText(ev))
}
