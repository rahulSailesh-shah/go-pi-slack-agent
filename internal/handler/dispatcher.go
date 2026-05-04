package handler

import (
	"log"
	"sync"

	"slack-agent/internal/store"
)

type Processor func(msg store.Message, files []store.File)

type Config struct {
	BufferSize int
	Processor  Processor
}

type event struct {
	msg   store.Message
	files []store.File
}

type channelWorker struct {
	ch     chan event
	stopCh chan struct{}
	done   chan struct{}
}

type Dispatcher struct {
	mu        sync.Mutex
	workers   map[string]*channelWorker
	processor Processor
	bufSize   int
	closed    bool
}

var _ Handler = (*Dispatcher)(nil)

func NewDispatcher(cfg Config) *Dispatcher {
	bs := cfg.BufferSize
	if bs <= 0 {
		bs = 64
	}
	return &Dispatcher{
		workers:   make(map[string]*channelWorker),
		processor: cfg.Processor,
		bufSize:   bs,
	}
}

func (d *Dispatcher) HandleEvent(msg store.Message, files []store.File) {
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

	w := &channelWorker{
		ch:     make(chan event, d.bufSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	d.workers[channelID] = w

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

func (d *Dispatcher) processEvent(evt event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("handler: processor panic on channel %s: %v", evt.msg.ChannelID, r)
		}
	}()
	d.processor(evt.msg, evt.files)
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
			d.processEvent(evt)
			select {
			case <-w.stopCh:
				drainDiscard(w.ch)
				return
			default:
			}
		}
	}
}
