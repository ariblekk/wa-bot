package storage

import (
	"strings"
	"sync"
	"time"
)

// ConversationMessage is deliberately compatible with an OpenAI chat message,
// while keeping the storage package independent from the OpenAI service.
type ConversationMessage struct {
	Role    string
	Content string
}

type conversation struct {
	Messages []ConversationMessage
	Touched  time.Time
}

// ConversationStore keeps a bounded, per-chat transcript in memory. It is
// suitable for one process; use Redis/DB when running multiple bot instances.
type ConversationStore struct {
	mu          sync.Mutex
	chats       map[string]conversation
	maxMessages int
	ttl         time.Duration
}

func NewConversationStore(maxMessages int, ttl time.Duration) *ConversationStore {
	if maxMessages < 1 {
		maxMessages = 1
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &ConversationStore{
		chats:       make(map[string]conversation),
		maxMessages: maxMessages,
		ttl:         ttl,
	}
}

// AddUserAndGetContext appends the incoming user message and returns the full
// bounded context, including the new message. The assistant reply is added
// only after the model call succeeds, so failures do not poison the transcript.
func (s *ConversationStore) AddUserAndGetContext(chatID, content string) []ConversationMessage {
	return s.add(chatID, ConversationMessage{Role: "user", Content: content})
}

func (s *ConversationStore) AddAssistant(chatID, content string) {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(content) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entry, ok := s.chats[chatID]
	if !ok || now.Sub(entry.Touched) > s.ttl {
		entry = conversation{}
	}
	entry.Messages = append(entry.Messages, ConversationMessage{Role: "assistant", Content: content})
	entry.Messages = trimMessages(entry.Messages, s.maxMessages)
	entry.Touched = now
	s.chats[chatID] = entry
}

func (s *ConversationStore) add(chatID string, message ConversationMessage) []ConversationMessage {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(message.Content) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entry, ok := s.chats[chatID]
	if !ok || now.Sub(entry.Touched) > s.ttl {
		entry = conversation{}
	}
	entry.Messages = append(entry.Messages, message)
	entry.Messages = trimMessages(entry.Messages, s.maxMessages)
	entry.Touched = now
	s.chats[chatID] = entry
	return cloneMessages(entry.Messages)
}

// RemoveLastUser rolls back a user turn when the model call failed. It only
// removes it if it is still the newest turn, so a later concurrent message is
// never deleted accidentally.
func (s *ConversationStore) RemoveLastUser(chatID, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.chats[chatID]
	if !ok || len(entry.Messages) == 0 {
		return
	}
	last := entry.Messages[len(entry.Messages)-1]
	if last.Role == "user" && last.Content == content {
		entry.Messages = entry.Messages[:len(entry.Messages)-1]
		entry.Touched = time.Now()
		if len(entry.Messages) == 0 {
			delete(s.chats, chatID)
		} else {
			s.chats[chatID] = entry
		}
	}
}

func (s *ConversationStore) Clear(chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chats, chatID)
}

func (s *ConversationStore) Len(chatID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.chats[chatID]
	if !ok || time.Since(entry.Touched) > s.ttl {
		return 0
	}
	return len(entry.Messages)
}

func trimMessages(messages []ConversationMessage, max int) []ConversationMessage {
	if len(messages) <= max {
		return messages
	}
	return messages[len(messages)-max:]
}

func cloneMessages(messages []ConversationMessage) []ConversationMessage {
	result := make([]ConversationMessage, len(messages))
	copy(result, messages)
	return result
}
