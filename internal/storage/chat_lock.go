package storage

import (
	"strings"
	"sync"
)

// ChatLocker serializes processing for each chat while allowing different
// chats to continue in parallel. It protects message ordering within one bot
// process; PostgreSQL remains the persistent source of conversation history.
type ChatLocker struct {
	mu    sync.Mutex
	chats map[string]*chatLockEntry
}

type chatLockEntry struct {
	mu   sync.Mutex
	refs int
}

func NewChatLocker() *ChatLocker {
	return &ChatLocker{chats: make(map[string]*chatLockEntry)}
}

// Acquire blocks until the specified chat can be processed and returns a
// release function. Empty chat IDs are treated as an independent no-op key.
func (l *ChatLocker) Acquire(chatID string) func() {
	key := strings.TrimSpace(chatID)
	if key == "" {
		return func() {}
	}

	l.mu.Lock()
	entry := l.chats[key]
	if entry == nil {
		entry = &chatLockEntry{}
		l.chats[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.chats, key)
		}
		l.mu.Unlock()
	}
}
