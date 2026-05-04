package store

import (
	"context"
	"sync"
	"time"
)

type Deduplicator interface {
	IsDuplicate(key string) bool
}

type ttlDeduplicator struct {
	mu      sync.RWMutex
	entries map[string]time.Time
	ttl     time.Duration
}

func newTTLDeduplicator(ctx context.Context, ttl time.Duration) *ttlDeduplicator {
	d := &ttlDeduplicator{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
	go d.sweep(ctx)
	return d
}

func (d *ttlDeduplicator) sweep(ctx context.Context) {
	ticker := time.NewTicker(d.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.mu.Lock()
			now := time.Now()
			for key, timestamp := range d.entries {
				if now.Sub(timestamp) > d.ttl {
					delete(d.entries, key)
				}
			}
			d.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (d *ttlDeduplicator) IsDuplicate(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.entries[key]; ok {
		return true
	}

	d.entries[key] = time.Now()
	return false
}
