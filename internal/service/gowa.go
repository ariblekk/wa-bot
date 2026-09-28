package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"wa-chatbot/internal/config"
	"wa-chatbot/internal/model"
)

type GOWAService struct {
	cfg    config.Config
	client *http.Client
	logger *log.Logger
}

func NewGOWAService(cfg config.Config, logger *log.Logger) *GOWAService {
	return &GOWAService{cfg: cfg, client: &http.Client{Timeout: cfg.GOWATimeout}, logger: logger}
}

type readMessageRequest struct {
	Phone string `json:"phone"`
}

type chatPresenceRequest struct {
	Phone  string `json:"phone"`
	Action string `json:"action"`
}

func (s *GOWAService) MarkRead(ctx context.Context, deviceID, phone, messageID string) error {
	if strings.TrimSpace(messageID) == "" {
		return nil
	}
	path := strings.ReplaceAll(s.cfg.GOWAReadMessagePath, "{message_id}", messageID)
	return s.postJSON(ctx, deviceID, path, readMessageRequest{Phone: phone})
}

func (s *GOWAService) SetChatPresence(ctx context.Context, deviceID, phone, action string) error {
	if action != "start" && action != "stop" {
		return fmt.Errorf("invalid chat presence action %q", action)
	}
	return s.postJSON(ctx, deviceID, s.cfg.GOWAChatPresencePath, chatPresenceRequest{Phone: phone, Action: action})
}

type downloadMediaResponse struct {
	Results struct {
		Filename  string `json:"filename"`
		FilePath  string `json:"file_path"`
		FileURL   string `json:"file_url"`
		FileSize  int64  `json:"file_size"`
		MediaType string `json:"media_type"`
	} `json:"results"`
}

// DownloadImage returns the raw bytes of an inbound image. It asks GoWA to
// fetch the media and then reads it back from the public statics URL.
func (s *GOWAService) DownloadImage(ctx context.Context, deviceID, phone, messageID string) ([]byte, string, error) {
	path := strings.ReplaceAll(s.cfg.GOWADownloadPath, "{message_id}", messageID) + "?phone=" + url.QueryEscape(phone)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint(path), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create GoWA download request: %w", err)
	}
	s.applyAuth(req, deviceID)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("call GoWA download: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read GoWA download response: %w", err)
	}
	if s.cfg.Debug {
		s.logger.Printf("gowa download message_id=%s status=%d response=%s", messageID, resp.StatusCode, truncate(string(body), 400))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("GoWA download returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var result downloadMediaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("decode GoWA download response: %w", err)
	}
	mediaURL := result.Results.FileURL
	if strings.TrimSpace(mediaURL) == "" {
		return nil, "", fmt.Errorf("GoWA did not return a media URL for message %s", messageID)
	}
	return s.fetchMedia(ctx, mediaURL, result.Results.Filename)
}

func (s *GOWAService) fetchMedia(ctx context.Context, mediaURL, filename string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create media request: %w", err)
	}
	if s.cfg.GOWABasicAuth != "" {
		if parts := strings.SplitN(s.cfg.GOWABasicAuth, ":", 2); len(parts) == 2 {
			req.SetBasicAuth(parts[0], parts[1])
		}
	}
	if s.cfg.GOWAAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.GOWAAPIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download media: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download media returned HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, int64(s.cfg.ImageMaxBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("read media: %w", err)
	}
	if len(data) > s.cfg.ImageMaxBytes {
		return nil, "", fmt.Errorf("image exceeds IMAGE_MAX_BYTES (%d bytes)", s.cfg.ImageMaxBytes)
	}
	return data, detectImageMIME(resp.Header.Get("Content-Type"), filename), nil
}

func detectImageMIME(header, filename string) string {
	if mimeType := strings.TrimSpace(strings.Split(header, ";")[0]); strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

func (s *GOWAService) postJSON(ctx context.Context, deviceID, path string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode GoWA request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(path), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create GoWA request: %w", err)
	}
	s.applyAuth(req, deviceID)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("call GoWA %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read GoWA response: %w", err)
	}
	if s.cfg.Debug {
		s.logger.Printf("gowa call path=%s status=%d response=%s", path, resp.StatusCode, truncate(string(body), 300))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GoWA %s returned HTTP %d: %s", path, resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

func (s *GOWAService) endpoint(path string) string {
	return strings.TrimRight(s.cfg.GOWABaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (s *GOWAService) applyAuth(req *http.Request, deviceID string) {
	req.Header.Set("Content-Type", "application/json")
	s.applyAuthDevice(req, deviceID)
}

// applyAuthDevice only adds the device and credential headers, leaving the
// content type to the caller so multipart bodies keep their boundary.
func (s *GOWAService) applyAuthDevice(req *http.Request, deviceID string) {
	if s.cfg.GOWADeviceID != "" {
		deviceID = s.cfg.GOWADeviceID
	}
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	if s.cfg.GOWAAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.GOWAAPIKey)
	}
	if s.cfg.GOWABasicAuth != "" {
		parts := strings.SplitN(s.cfg.GOWABasicAuth, ":", 2)
		if len(parts) == 2 {
			req.SetBasicAuth(parts[0], parts[1])
		}
	}
}

func (s *GOWAService) SendText(ctx context.Context, deviceID, phone, message string) error {
	return s.postJSON(ctx, deviceID, s.cfg.GOWASendPath, model.SendMessageRequest{Phone: phone, Message: message})
}

// SendVideo hands GoWA a URL and lets it fetch the file server side, so the
// bot never has to buffer the media itself.
func (s *GOWAService) SendVideo(ctx context.Context, deviceID, phone, fileURL, caption, replyTo string) error {
	fields := map[string]string{
		"phone":     phone,
		"video_url": fileURL,
	}
	if caption != "" {
		fields["caption"] = caption
	}
	if replyTo != "" {
		fields["reply_message_id"] = replyTo
	}
	return s.postForm(ctx, deviceID, s.cfg.GOWASendVideoPath, fields)
}

// SendAudio sends a downloaded audio file. voiceNote wraps it as a WhatsApp
// voice message, which needs ffmpeg on the GoWA host.
func (s *GOWAService) SendAudio(ctx context.Context, deviceID, phone, fileURL string, voiceNote bool) error {
	fields := map[string]string{
		"phone":     phone,
		"audio_url": fileURL,
	}
	if voiceNote {
		fields["ptt"] = "true"
	}
	return s.postForm(ctx, deviceID, s.cfg.GOWASendAudioPath, fields)
}

func (s *GOWAService) postForm(ctx context.Context, deviceID, path string, fields map[string]string) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("encode GoWA form field %s: %w", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize GoWA form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(path), body)
	if err != nil {
		return fmt.Errorf("create GoWA request: %w", err)
	}
	// applyAuth sets a JSON content type, so the multipart boundary is set here.
	req.Header.Del("Content-Type")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	s.applyAuthDevice(req, deviceID)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("call GoWA %s: %w", path, err)
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read GoWA response: %w", err)
	}
	if s.cfg.Debug {
		s.logger.Printf("gowa call path=%s status=%d response=%s", path, resp.StatusCode, truncate(string(response), 300))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GoWA %s returned HTTP %d: %s", path, resp.StatusCode, truncate(string(response), 500))
	}
	return nil
}
