package runner

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/sandbox"
	"slack-agent/internal/session"
	msglog "slack-agent/internal/store"
	"slack-agent/internal/tools"
)

type Config struct {
	DataDir      string
	SystemPrompt string
	// SystemPromptBuilder, when non-nil, builds the system prompt per channel.
	// If it returns empty string, SystemPrompt is used.
	SystemPromptBuilder func(channelID string) string
	// ExtraTools returns additional agent tools for this channel (e.g. slack_post).
	ExtraTools func(channelID string) []agent.AgentTool
	Provider   agent.Provider
	ModelName  string
	Executor   sandbox.Executor
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

	toolSet := tools.NewToolSet(f.cfg.Executor, channelID)
	if f.cfg.ExtraTools != nil {
		if extras := f.cfg.ExtraTools(channelID); len(extras) > 0 {
			toolSet = append(toolSet, extras...)
		}
	}

	systemPrompt := f.cfg.SystemPrompt
	if f.cfg.SystemPromptBuilder != nil {
		if p := strings.TrimSpace(f.cfg.SystemPromptBuilder(channelID)); p != "" {
			systemPrompt = p
		}
	}

	a := agent.NewAgent(
		agent.WithInitialState(&agent.AgentState{
			SystemPrompt: systemPrompt,
			Provider:     f.cfg.Provider,
			ModelName:    f.cfg.ModelName,
			Tools:        toolSet,
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
