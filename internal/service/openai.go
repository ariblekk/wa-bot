package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"wa-chatbot/internal/config"
)

type OpenAIService struct {
	cfg    config.Config
	client *http.Client
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// ConversationTurn is the small public type the webhook handler uses to pass
// per-chat history into the OpenAI request.
type ConversationTurn struct {
	Role    string
	Content string
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imageURLPart struct {
	Type     string   `json:"type"`
	ImageURL imageRef `json:"image_url"`
}

type imageRef struct {
	URL string `json:"url"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewOpenAIService(cfg config.Config) *OpenAIService {
	return &OpenAIService{cfg: cfg, client: &http.Client{Timeout: cfg.OpenAITimeout}}
}

// ReplyWithImage sends a text prompt together with an inline base64 image so
// vision-capable models can describe or answer questions about the picture.
func (s *OpenAIService) ReplyWithImage(ctx context.Context, userMessage string, imageData []byte, mimeType string) (string, error) {
	return s.ReplyWithImageContext(ctx, userMessage, imageData, mimeType, nil)
}

func (s *OpenAIService) ReplyWithImageContext(ctx context.Context, userMessage string, imageData []byte, mimeType string, history []ConversationTurn) (string, error) {
	parts := []any{textPart{Type: "text", Text: userMessage}}
	if len(imageData) > 0 {
		parts = append(parts, imageURLPart{
			Type:     "image_url",
			ImageURL: imageRef{URL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageData)},
		})
	}
	messages := make([]chatMessage, 0, len(history)+1)
	for _, turn := range history {
		if strings.TrimSpace(turn.Role) == "" || strings.TrimSpace(turn.Content) == "" {
			continue
		}
		messages = append(messages, chatMessage{Role: turn.Role, Content: turn.Content})
	}
	messages = append(messages, chatMessage{Role: "user", Content: parts})
	return s.completeMessages(ctx, messages)
}

func (s *OpenAIService) Reply(ctx context.Context, userMessage string) (string, error) {
	return s.ReplyWithContext(ctx, userMessage, nil)
}

// ReplyWithContext sends the current user message together with the previous
// turns for the same chat.
func (s *OpenAIService) ReplyWithContext(ctx context.Context, userMessage string, history []ConversationTurn) (string, error) {
	messages := make([]chatMessage, 0, len(history)+1)
	for _, turn := range history {
		if strings.TrimSpace(turn.Role) == "" || strings.TrimSpace(turn.Content) == "" {
			continue
		}
		messages = append(messages, chatMessage{Role: turn.Role, Content: turn.Content})
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" || messages[len(messages)-1].Content != userMessage {
		messages = append(messages, chatMessage{Role: "user", Content: userMessage})
	}
	return s.completeMessages(ctx, messages)
}

func (s *OpenAIService) systemPrompt() string {
	location := s.cfg.AppTimezone
	if location == nil {
		location = time.Local
	}
	now := time.Now().In(location)
	name := s.cfg.AppTimezoneName
	if strings.TrimSpace(name) == "" {
		name = location.String()
	}
	return s.cfg.OpenAISystemPrompt + "\n\n" +
		"KONTEKS WAKTU SERVER (data aktual dari backend):\n" +
		"- Waktu server saat ini: " + now.Format("02 January 2006, 15:04:05") + "\n" +
		"- Zona waktu server: " + name + "\n" +
		"Jika pengguna menanyakan waktu atau tanggal sekarang, jawab langsung berdasarkan konteks waktu server ini. Jangan mengatakan tidak bisa melihat waktu realtime jika pertanyaannya tentang waktu server. Jangan mengklaim bisa melihat jam perangkat pengguna. Jika pengguna menanyakan jam perangkat mereka, jelaskan bahwa yang tersedia adalah waktu server."
}

func (s *OpenAIService) complete(ctx context.Context, userContent any) (string, error) {
	return s.completeMessages(ctx, []chatMessage{{Role: "user", Content: userContent}})
}

func (s *OpenAIService) completeMessages(ctx context.Context, messages []chatMessage) (string, error) {
	requestBody := chatRequest{
		Model:     s.cfg.OpenAIModel,
		Messages:  append([]chatMessage{{Role: "system", Content: s.systemPrompt()}}, messages...),
		MaxTokens: s.cfg.OpenAIMaxTokens,
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("encode OpenAI request: %w", err)
	}

	endpoint := s.cfg.OpenAIChatURL
	if endpoint == "" {
		endpoint = strings.TrimRight(s.cfg.OpenAIBaseURL, "/") + "/" + strings.TrimLeft(s.cfg.OpenAIChatPath, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("create OpenAI request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.OpenAIAPIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call OpenAI: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("OpenAI returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result chatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode OpenAI response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("OpenAI error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("OpenAI response contains no message")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
