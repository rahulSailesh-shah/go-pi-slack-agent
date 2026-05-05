package runner

import (
	"log"
	"path/filepath"
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/session"
	msglog "slack-agent/internal/store"
)

type Config struct {
	DataDir      string
	SystemPrompt string
	Provider     agent.Provider
	ModelName    string
	Tools        []agent.AgentTool
}

type Factory struct {
	cfg Config
}

func New(cfg Config) *Factory {
	return &Factory{cfg: cfg}
}

func (f *Factory) Create(channelID string) (*session.AgentSession, []msglog.Message) {
	contextPath := filepath.Join(f.cfg.DataDir, channelID, "context.jsonl")

	st, err := session.NewJSONLStore(contextPath)
	if err != nil {
		log.Printf("runner: open store for %s: %v", channelID, err)
		return nil, nil
	}

	manager, err := session.NewSessionManager(st)
	if err != nil {
		log.Printf("runner: open manager for %s: %v", channelID, err)
		return nil, nil
	}

	a := agent.NewAgent(
		agent.WithInitialState(&agent.AgentState{
			SystemPrompt: f.cfg.SystemPrompt,
			Provider:     f.cfg.Provider,
			ModelName:    f.cfg.ModelName,
			Tools:        f.cfg.Tools,
		}),
	)

	sessionCtx := manager.BuildSessionContext()
	if len(sessionCtx.Messages) > 0 {
		a.ReplaceMessages(sessionCtx.Messages)
	}

	sess := session.NewAgentSession(session.Config{
		Agent:   a,
		Manager: manager,
	})
	if sess == nil {
		log.Printf("runner: NewAgentSession returned nil for %s", channelID)
		return nil, nil
	}

	var cutoff time.Time
	if !st.IsNew() {
		entries := manager.GetEntries()
		if len(entries) == 0 {
			cutoff = time.Now()
		} else {
			lastEntry := entries[len(entries)-1]
			if t, err := time.Parse(time.RFC3339, lastEntry.EntryTimestamp()); err == nil {
				cutoff = t
			}
		}
	}

	pending := f.syncFromLog(channelID, cutoff)
	return sess, pending
}

func (f *Factory) syncFromLog(channelID string, cutoff time.Time) []msglog.Message {
	if cutoff.IsZero() {
		return nil
	}
	msgs, err := msglog.LoadMessages(f.cfg.DataDir, channelID)
	if err != nil {
		log.Printf("runner: load log for %s: %v", channelID, err)
		return nil
	}
	var pending []msglog.Message
	for _, m := range msgs {
		if m.Timestamp.After(cutoff) {
			pending = append(pending, m)
		}
	}
	return pending
}
