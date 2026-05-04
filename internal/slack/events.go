package slack

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"slack-agent/internal/store"
)

// rawFile holds file attachment fields extracted from a Slack event payload.
// client.go populates this from slack.Msg.Files before calling toFiles.
type rawFile struct {
	Name               string
	URLPrivate         string
	URLPrivateDownload string
	Mimetype           string
}

func toMessage(channelID, userID, ts, text string) store.Message {
	tsFloat, _ := strconv.ParseFloat(ts, 64)
	return store.Message{
		ID:        ts,
		ChannelID: channelID,
		Timestamp: time.Unix(int64(tsFloat), 0),
		UserID:    userID,
		Text:      stripBotMention(text),
		Platform:  "slack",
	}
}

func toFiles(files []rawFile) []store.File {
	result := make([]store.File, 0, len(files))
	for _, f := range files {
		url := f.URLPrivateDownload
		if url == "" {
			url = f.URLPrivate
		}
		if url == "" || f.Name == "" {
			continue
		}
		result = append(result, store.File{
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
