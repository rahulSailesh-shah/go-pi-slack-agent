package slack

import "slack-agent/internal/store"

// Handler is implemented by the caller to respond to Slack events.
// The slack package handles logging, attachment queuing, and goroutine
// dispatch. Per-channel serialization and busy-state are the handler's
// responsibility — required for correctness when multiple platforms share
// the same agent.
type Handler interface {
	HandleStop(channelID string)
	HandleEvent(msg store.Message, files []store.File)
}
