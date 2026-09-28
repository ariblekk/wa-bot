package model

import (
	"encoding/json"
	"strings"
)

// Media represents an inbound attachment. GoWA sends it either as a plain
// string (local file path) or as an object with path/url plus a caption.
type Media struct {
	Path    string
	URL     string
	Caption string
}

func (m *Media) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		return json.Unmarshal(data, &m.Path)
	}

	var raw struct {
		Path    string `json:"path"`
		URL     string `json:"url"`
		Caption string `json:"caption"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Path = raw.Path
	m.URL = raw.URL
	m.Caption = raw.Caption
	return nil
}

func (m Media) Present() bool {
	return strings.TrimSpace(m.Path) != "" || strings.TrimSpace(m.URL) != ""
}

type WebhookEvent struct {
	Event     string          `json:"event"`
	DeviceID  string          `json:"device_id"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   IncomingMessage `json:"payload"`
}

type IncomingMessage struct {
	ID                string `json:"id"`
	ChatID            string `json:"chat_id"`
	From              string `json:"from"`
	Body              string `json:"body"`
	IsFromMe          bool   `json:"is_from_me"`
	IsGroup           bool   `json:"is_group"`
	SenderDisplayName string `json:"sender_display_name"`
	FromName          string `json:"from_name"`
	Timestamp         string `json:"timestamp"`
	Image             Media  `json:"image"`
}

type SendMessageRequest struct {
	Phone   string `json:"phone"`
	Message string `json:"message"`
}
