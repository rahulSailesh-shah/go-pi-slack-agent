package media

import msglog "slack-agent/internal/store"

type FileHandler interface {
	ProcessAttachments(channelID string, files []msglog.File, timestamp string) []msglog.Attachment
	Close() error
}
