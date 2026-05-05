package handler

import msglog "slack-agent/internal/store"

type Handler interface {
	HandleStop(channelID string)
	HandleEvent(msg msglog.Message, files []msglog.File)
}
