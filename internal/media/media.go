package media

import "slack-agent/internal/store"

type FileHandler interface {
	ProcessAttachments(channelID string, files []store.File, timestamp string) []store.Attachment
	Close() error
}
