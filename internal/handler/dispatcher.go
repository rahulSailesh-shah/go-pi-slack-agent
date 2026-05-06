package handler

import (
	"context"
	"log"
	"strings"
	"sync"

	agent "github.com/rahulSailesh-shah/go-pi-agent"

	"slack-agent/internal/session"
	msglog "slack-agent/internal/store"
)

type Config struct {
	BufferSize          int
	SessionFactory      func(channelID string) (*session.AgentSession, []msglog.Message)
	Responder           func(channelID, text string) error
	SystemPromptBuilder func(channelID string) string
}

type event struct {
	msg   msglog.Message
	files []msglog.File
}

type channelWorker struct {
	channelID string
	session   *session.AgentSession
	ch        chan event
	stopCh    chan struct{}
	done      chan struct{}
}

type Dispatcher struct {
	mu      sync.Mutex
	workers map[string]*channelWorker
	cfg     Config
	closed  bool
}

var _ Handler = (*Dispatcher)(nil)

func NewDispatcher(cfg Config) *Dispatcher {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}
	return &Dispatcher{
		workers: make(map[string]*channelWorker),
		cfg:     cfg,
	}
}

func (d *Dispatcher) HandleEvent(msg msglog.Message, files []msglog.File) {
	w, alreadyQueued := d.getOrCreateWorker(msg.ChannelID, msg.ID)
	if w == nil {
		log.Printf("handler: drop message %s for channel %s (worker unavailable)", msg.ID, msg.ChannelID)
		return
	}
	if alreadyQueued {
		log.Printf("handler: skip duplicate enqueue for message %s on channel %s (already in pending replay)", msg.ID, msg.ChannelID)
		return
	}
	select {
	case w.ch <- event{msg: msg, files: files}:
		log.Printf("handler: enqueued live message %s on channel %s", msg.ID, msg.ChannelID)
	default:
		log.Printf("handler: channel %s queue full, dropping message %s", msg.ChannelID, msg.ID)
	}
}

func (d *Dispatcher) HandleStop(channelID string) {
	d.mu.Lock()
	w, ok := d.workers[channelID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.workers, channelID)
	d.mu.Unlock()

	if w.session != nil {
		w.session.Abort()
		w.session.Dispose()
	}
	close(w.stopCh)
}

func (d *Dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	workers := make([]*channelWorker, 0, len(d.workers))
	for _, w := range d.workers {
		workers = append(workers, w)
	}
	d.workers = nil
	d.mu.Unlock()

	for _, w := range workers {
		if w.session != nil {
			w.session.Abort()
			w.session.Dispose()
		}
		close(w.stopCh)
	}
}

func (d *Dispatcher) getOrCreateWorker(channelID, incomingMsgID string) (*channelWorker, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		log.Printf("handler: dispatcher closed, cannot create worker for channel %s", channelID)
		return nil, false
	}

	if w, ok := d.workers[channelID]; ok {
		log.Printf("handler: reusing worker for channel %s", channelID)
		return w, false
	}

	sess, pending := d.cfg.SessionFactory(channelID)
	if sess == nil {
		log.Printf("handler: session factory returned nil for channel %s", channelID)
		return nil, false
	}
	log.Printf("handler: created new worker for channel %s", channelID)

	w := &channelWorker{
		channelID: channelID,
		session:   sess,
		ch:        make(chan event, d.cfg.BufferSize),
		stopCh:    make(chan struct{}),
		done:      make(chan struct{}),
	}
	d.workers[channelID] = w

	// Subscribe once — fires in agent goroutine on each run.
	sess.Subscribe(func(evt agent.AgentEvent) {
		e, ok := evt.(agent.AgentEnd)
		if !ok {
			return
		}
		for i := len(e.Messages) - 1; i >= 0; i-- {
			msg, ok := e.Messages[i].(agent.AssistantMessage)
			if !ok {
				continue
			}
			var sb strings.Builder
			for _, c := range msg.Contents {
				if t, ok := c.(agent.TextContent); ok {
					sb.WriteString(t.Text)
				}
			}
			if text := sb.String(); text != "" {
				if err := d.cfg.Responder(channelID, text); err != nil {
					log.Printf("handler: responder error for channel %s: %v", channelID, err)
				}
			}
			break
		}
	})

	alreadyQueued := pendingContainsMessageID(pending, incomingMsgID)
	log.Printf("handler: replay pending for channel %s count=%d incoming=%s alreadyQueued=%t",
		channelID, len(pending), incomingMsgID, alreadyQueued)
	for _, msg := range pending {
		select {
		case w.ch <- event{msg: msg}:
			log.Printf("handler: replayed pending message %s on channel %s", msg.ID, channelID)
		default:
			log.Printf("handler: buffer full, dropping recovered message %s for channel %s", msg.ID, channelID)
		}
	}

	go d.runWorker(w)
	return w, alreadyQueued
}

func pendingContainsMessageID(pending []msglog.Message, messageID string) bool {
	if messageID == "" {
		return false
	}
	for _, msg := range pending {
		if msg.ID == messageID {
			return true
		}
	}
	return false
}

func drainDiscard(ch <-chan event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (d *Dispatcher) processEvent(w *channelWorker, evt event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("handler: panic on channel %s: %v", evt.msg.ChannelID, r)
		}
	}()

	if d.cfg.SystemPromptBuilder != nil {
		if p := strings.TrimSpace(d.cfg.SystemPromptBuilder(evt.msg.ChannelID)); p != "" {
			w.session.SetSystemPrompt(p)
		}
	}
	text := buildUserMessage(evt.msg)
	if err := w.session.Prompt(context.Background(), text); err != nil {
		log.Printf("handler: prompt error on channel %s: %v", evt.msg.ChannelID, err)
	}
}

func (d *Dispatcher) runWorker(w *channelWorker) {
	defer close(w.done)
	for {
		select {
		case <-w.stopCh:
			drainDiscard(w.ch)
			return
		case evt, ok := <-w.ch:
			if !ok {
				return
			}
			d.processEvent(w, evt)
			select {
			case <-w.stopCh:
				drainDiscard(w.ch)
				return
			default:
			}
		}
	}
}

// buildUserMessage formats the Slack message for the LLM.
func buildUserMessage(msg msglog.Message) string {
	var sb strings.Builder
	if msg.UserName != "" {
		sb.WriteString("[")
		sb.WriteString(msg.UserName)
		sb.WriteString("]: ")
	}
	sb.WriteString(msg.Text)
	return sb.String()
}
