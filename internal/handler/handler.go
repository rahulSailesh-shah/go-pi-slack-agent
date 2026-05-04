package handler

import "slack-agent/internal/store"

type Handler interface {
	HandleStop(channelID string)
	HandleEvent(msg store.Message, files []store.File)
}
