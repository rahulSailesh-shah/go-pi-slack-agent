package agentsession

import (
	"context"
	"regexp"
	"slack-agent/internal/sessionlog"
	"sync"

	agent "github.com/rahulSailesh-shah/go-pi-agent"
)

type Compactor interface {
	PrepareCompaction(pathEntries []sessionlog.Entry) *CompactionPreparation
	Compact(ctx context.Context, preparation *CompactionPreparation) (*sessionlog.CompactionEntry, error)
}

type CompactionPreparation struct {
	EntriesToSummarize []sessionlog.Entry
	EntriesToKeep      []sessionlog.Entry
	FirstKeptEntryID   string
	TokensBefore       int
}

type EventListener func(event agent.AgentEvent)

type AgentSession struct {
	agent          *agent.Agent
	sessionManager sessionlog.Manager
	compactor      Compactor

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
	Agent          *agent.Agent
	SessionManager sessionlog.Manager
	Compactor      Compactor
}

func NewAgentSession(config Config) *AgentSession {
	s := &AgentSession{
		agent:          config.Agent,
		sessionManager: config.SessionManager,
		compactor:      config.Compactor,
		listeners:      make(map[int]EventListener),
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
			if _, err := s.sessionManager.AppendMessage(message); err != nil {
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
		// s.checkCompaction(lastAssistant, false)
	}

	if err := s.agent.Prompt(ctx, text, images...); err != nil {
		return err
	}

	s.waitForRetry()

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

// Abort cancels any in-progress operation (stream, retry, compaction).
func (s *AgentSession) Abort() {
	s.abortRetry()
	// s.abortCompaction()
	s.agent.Abort()
}

// retryablePattern matches error messages that should be retried.
var retryablePattern = regexp.MustCompile(
	`(?i)overloaded|rate.?limit|too many requests|429|500|502|503|504|` +
		`service.?unavailable|server error|internal error|connection.?error|` +
		`connection.?refused|other side closed|fetch failed|upstream.?connect|` +
		`reset before headers|terminated|retry delay`,
)

func (s *AgentSession) isRetryableError(msg *agent.Message) bool {
	// TODO: Check for stop reason and determine if it is retryable
	return true
}

func (s *AgentSession) handleRetryableError(msg *agent.Message) bool {
	// TODO: implements exponential backoff retry.
	//  Returns true if a retry was initiated, false if disabled or max exceeded.

	return true
}

func (s *AgentSession) abortRetry() {
	s.retryMu.Lock()
	if s.retryCancel != nil {
		s.retryCancel()
		s.retryCancel = nil
	}
	s.retryMu.Unlock()
	s.resolveRetry()
}

func (s *AgentSession) waitForRetry() {
	s.retryMu.Lock()
	done := s.retryDone
	s.retryMu.Unlock()

	if done != nil {
		<-done
	}
}

func (s *AgentSession) resolveRetry() {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if s.retryDone != nil {
		select {
		case <-s.retryDone:
			// already closed
		default:
			close(s.retryDone)
		}
		s.retryDone = nil
	}
}

func (s *AgentSession) findLastAssistantMessage() *agent.Message {
	messages := s.agent.State().Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role() == "assistant" {
			return &messages[i]
		}
	}
	return nil
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
