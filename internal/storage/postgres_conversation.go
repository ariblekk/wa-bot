package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const conversationSchema = `
CREATE TABLE IF NOT EXISTS conversation_messages (
    id BIGSERIAL PRIMARY KEY,
    chat_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversation_messages_chat_created
    ON conversation_messages (chat_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_conversation_messages_created
    ON conversation_messages (created_at);
`

// PostgresConversationStore persists chat memory in PostgreSQL. The store
// returns the last N messages in chronological order for the OpenAI API.
type PostgresConversationStore struct {
	pool        *pgxpool.Pool
	maxMessages int
	ttl         time.Duration
}

func NewPostgresConversationStore(ctx context.Context, dsn string, maxMessages int, ttl time.Duration, maxConns, minConns int) (*PostgresConversationStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is empty")
	}
	if maxMessages < 1 {
		maxMessages = 1
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DSN: %w", err)
	}
	if maxConns > 0 {
		poolConfig.MaxConns = int32(maxConns)
	}
	if minConns > 0 {
		poolConfig.MinConns = int32(minConns)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	store := &PostgresConversationStore{pool: pool, maxMessages: maxMessages, ttl: ttl}
	if _, err := pool.Exec(ctx, conversationSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create conversation schema: %w", err)
	}
	return store, nil
}

func (s *PostgresConversationStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresConversationStore) AddUserAndGetContext(ctx context.Context, chatID, content string) ([]ConversationMessage, error) {
	return s.addAndGet(ctx, chatID, ConversationMessage{Role: "user", Content: content})
}

func (s *PostgresConversationStore) AddAssistant(ctx context.Context, chatID, content string) error {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(content) == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO conversation_messages (chat_id, role, content)
SELECT $1, 'assistant', $2
WHERE EXISTS (
    SELECT 1 FROM conversation_messages
    WHERE chat_id = $1 AND role = 'user'
)`, chatID, content)
	return err
}

func (s *PostgresConversationStore) RemoveLastUser(ctx context.Context, chatID, content string) error {
	_, err := s.pool.Exec(ctx, `
DELETE FROM conversation_messages
WHERE id = (
    SELECT id FROM conversation_messages
    WHERE chat_id = $1 AND role = 'user' AND content = $2
    ORDER BY created_at DESC, id DESC
    LIMIT 1
)`, chatID, content)
	return err
}

func (s *PostgresConversationStore) Clear(ctx context.Context, chatID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM conversation_messages WHERE chat_id = $1`, chatID)
	return err
}

func (s *PostgresConversationStore) addAndGet(ctx context.Context, chatID string, message ConversationMessage) ([]ConversationMessage, error) {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(message.Content) == "" {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM conversation_messages WHERE chat_id = $1 AND created_at < NOW() - ($2 * INTERVAL '1 second')`, chatID, s.ttl.Seconds()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_messages (chat_id, role, content) VALUES ($1, $2, $3)`, chatID, message.Role, message.Content); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
SELECT role, content
FROM conversation_messages
WHERE chat_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2`, chatID, s.maxMessages)
	if err != nil {
		return nil, err
	}
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ConversationMessage, error) {
		var item ConversationMessage
		if err := row.Scan(&item.Role, &item.Content); err != nil {
			return ConversationMessage{}, err
		}
		return item, nil
	})
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return messages, nil
}
