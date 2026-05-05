package session

import (
	"context"
	"sync"

	agent "github.com/rahulSailesh-shah/go-pi-agent"
)

type EventListener func(event agent.AgentEvent)

type AgentSession struct {
	agent     *agent.Agent
	manager   *Manager
	compactor Compactor

	mu             sync.Mutex
	listeners      map[int]EventListener
	nextListenerID int
	unsubAgent     func()

	compactionCancel context.CancelFunc

	retryAttempt int
	retryCancel  context.CancelFunc
	retryDone    chan struct{}
	retryMu      sync.Mutex

	lastAssistantMsg agent.Message
	hasLastAssistant bool
}

type Config struct {
	Agent   *agent.Agent
	Manager *Manager
}

func NewAgentSession(config Config) *AgentSession {
	compactor, err := NewCompactor(nil)
	if err != nil {
		return nil
	}
	s := &AgentSession{
		agent:     config.Agent,
		manager:   config.Manager,
		compactor: compactor,
		listeners: make(map[int]EventListener),
	}
	s.unsubAgent = s.agent.Subscribe(s.handleAgentEvent)

	return s
}

func (s *AgentSession) Subscribe(listener EventListener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextListenerID
	s.nextListenerID++
	s.listeners[id] = listener

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

func (s *AgentSession) emit(event agent.AgentEvent) {
	s.mu.Lock()
	listeners := make([]EventListener, 0, len(s.listeners))
	for _, l := range s.listeners {
		listeners = append(listeners, l)
	}
	s.mu.Unlock()

	for _, l := range listeners {
		l(event)
	}
}

func (s *AgentSession) handleAgentEvent(event agent.AgentEvent) {
	s.emit(event)

	switch e := event.(type) {
	case agent.MessageEnd:
		message := e.Message
		if message.Role() == "user" || message.Role() == "assistant" || message.Role() == "tool" {
			if _, err := s.manager.AppendMessage(message); err != nil {
				_ = err // TODO: proper logging
			}
		}
		if message.Role() == "assistant" {
			s.lastAssistantMsg = message
			s.hasLastAssistant = true
			// 	TODO: Handle retry logic
		}
	case agent.AgentEnd:
		if s.hasLastAssistant {
			// msg := s.lastAssistantMsg
			// s.lastAssistantMsg = nil
			// TODO: Handle retry logic
			// TODO: Handle compaction
		}
	}
}

func (s *AgentSession) Prompt(ctx context.Context, text string, images ...agent.ImageContent) error {
	lastAssistant := s.findLastAssistantMessage()
	if lastAssistant != nil {
		s.checkCompaction(ctx, lastAssistant)
	}

	if err := s.agent.Prompt(ctx, text, images...); err != nil {
		return err
	}

	<-s.agent.WaitForIdle()
	return nil
}

func (s *AgentSession) Messages() []agent.Message {
	return s.agent.State().Messages
}

func (s *AgentSession) IsStreaming() bool {
	return s.agent.State().IsStreaming
}

func (s *AgentSession) SetSystemPrompt(prompt string) {
	s.agent.SetSystemPrompt(prompt)
}

// checks if compaction is needed based on the last assistant message, generates a summary and appends it to the session
func (s *AgentSession) checkCompaction(ctx context.Context, lastAssistant *agent.AssistantMessage) {
	if lastAssistant == nil || lastAssistant.StopReason == agent.StopReasonAborted {
		return
	}

	contextWindow := 200000

	latestCompaction := s.manager.GetLatestCompaction()
	if latestCompaction != nil && lastAssistant.Timestamp.Before(latestCompaction.Timestamp) {
		return
	}

	// Case 1: Overflow - LLM returned context overflow error
	if IsContextOverflow(lastAssistant, contextWindow) {
		s.runCompaction(ctx)
		return
	}

	// Case 2: Threshold - context is getting large.
	// For error messages (no usage data), estimate from last successful response.
	// This ensures sessions that hit persistent API errors can still compact.
	var contextTokens int
	if lastAssistant.StopReason == agent.StopReasonError {
		messages := s.agent.State().Messages
		lastUsage := findLastUsage(messages)
		if lastUsage == nil {
			return
		}
		if latestCompaction != nil && !lastUsage.Timestamp.After(latestCompaction.Timestamp) {
			return
		}
		contextTokens = estimateContextTokens(messages)
	} else {
		contextTokens = lastAssistant.Usage.TotalTokens
	}

	compactionThreshold := int(float64(contextWindow) * 0.80)
	if contextTokens >= compactionThreshold {
		s.runCompaction(ctx)
	}
}

func (s *AgentSession) runCompaction(ctx context.Context) {
	branch := s.manager.GetBranch(nil)
	if len(branch) == 0 {
		return
	}

	prep := s.compactor.PrepareCompaction(branch)
	if prep == nil {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	s.compactionCancel = cancel
	defer cancel()

	entry, err := s.compactor.Compact(ctx, prep)
	if err != nil {
		_ = err // TODO: logging
		return
	}

	if _, err := s.manager.AppendCompaction(
		entry.Summary,
		entry.FirstKeptEntryID,
		entry.TokensBefore,
		entry.Details,
	); err != nil {
		_ = err // TODO: logging
		return
	}

	sessionContext := s.manager.BuildSessionContext()
	s.agent.ReplaceMessages(sessionContext.Messages)
}

func (s *AgentSession) Abort() {
	// s.abortRetry()
	if s.compactionCancel != nil {
		s.compactionCancel()
	}
	s.agent.Abort()
}

func (s *AgentSession) Dispose() {
	if s.unsubAgent != nil {
		s.unsubAgent()
		s.unsubAgent = nil
	}
	s.mu.Lock()
	s.listeners = nil
	s.mu.Unlock()
}

func (s *AgentSession) findLastAssistantMessage() *agent.AssistantMessage {
	messages := s.agent.State().Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if am, ok := messages[i].(agent.AssistantMessage); ok {
			return &am
		}
	}
	return nil
}
