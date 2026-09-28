package storage

import (
	"sync"
	"time"
)

type Deduplicator struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	maxAge time.Duration
}

func NewDeduplicator(maxAge time.Duration) *Deduplicator {
	return &Deduplicator{seen: make(map[string]time.Time), maxAge: maxAge}
}

// First returns true only the first time a key is observed.
func (d *Deduplicator) First(key string) bool {
	if key == "" {
		return true
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	for existing, timestamp := range d.seen {
		if now.Sub(timestamp) > d.maxAge {
			delete(d.seen, existing)
		}
	}
	if _, exists := d.seen[key]; exists {
		return false
	}
	d.seen[key] = now
	return true
}
