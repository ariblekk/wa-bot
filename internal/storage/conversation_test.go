package storage

import (
	"testing"
	"time"
)

func TestConversationStoreKeepsChatContextBounded(t *testing.T) {
	store := NewConversationStore(3, time.Hour)
	store.AddUserAndGetContext("chat-a", "one")
	store.AddAssistant("chat-a", "reply one")
	store.AddUserAndGetContext("chat-a", "two")
	context := store.AddUserAndGetContext("chat-a", "three")

	if len(context) != 3 {
		t.Fatalf("context length = %d, want 3", len(context))
	}
	if context[0].Content != "reply one" || context[2].Content != "three" {
		t.Fatalf("unexpected bounded context: %#v", context)
	}
	if store.Len("chat-b") != 0 {
		t.Fatal("a different chat must not see chat-a history")
	}
}

func TestConversationStoreExpiresHistory(t *testing.T) {
	store := NewConversationStore(10, time.Millisecond)
	store.AddUserAndGetContext("chat-a", "hello")
	time.Sleep(5 * time.Millisecond)
	if got := store.Len("chat-a"); got != 0 {
		t.Fatalf("expired history length = %d", got)
	}
}

func TestConversationStoreRollsBackFailedUserMessage(t *testing.T) {
	store := NewConversationStore(10, time.Hour)
	store.AddUserAndGetContext("chat-a", "failed")
	store.RemoveLastUser("chat-a", "failed")
	if got := store.Len("chat-a"); got != 0 {
		t.Fatalf("history after rollback = %d", got)
	}
}

func TestConversationStoreDoesNotRollbackWrongTurn(t *testing.T) {
	store := NewConversationStore(10, time.Hour)
	store.AddUserAndGetContext("chat-a", "first")
	store.AddAssistant("chat-a", "answer")
	store.AddUserAndGetContext("chat-a", "second")
	store.RemoveLastUser("chat-a", "first")
	if got := store.Len("chat-a"); got != 3 {
		t.Fatalf("history should remain intact, got %d", got)
	}
}
