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
	BufferSize     int
	SessionFactory func(channelID string) (*session.AgentSession, []msglog.Message)
	Responder      func(channelID, text string) error
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
	w := d.getOrCreateWorker(msg.ChannelID)
	if w == nil {
		return
	}
	select {
	case w.ch <- event{msg: msg, files: files}:
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

func (d *Dispatcher) getOrCreateWorker(channelID string) *channelWorker {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}

	if w, ok := d.workers[channelID]; ok {
		return w
	}

	sess, pending := d.cfg.SessionFactory(channelID)
	if sess == nil {
		log.Printf("handler: session factory returned nil for channel %s", channelID)
		return nil
	}

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

	// Inject recovered messages before starting the goroutine so they are
	// processed before any newly arriving events.
	for _, msg := range pending {
		select {
		case w.ch <- event{msg: msg}:
		default:
			log.Printf("handler: buffer full, dropping recovered message %s for channel %s", msg.ID, channelID)
		}
	}

	go d.runWorker(w)
	return w
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
