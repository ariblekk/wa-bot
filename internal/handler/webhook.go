package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"wa-chatbot/internal/config"
	"wa-chatbot/internal/model"
	"wa-chatbot/internal/security"
	"wa-chatbot/internal/service"
	"wa-chatbot/internal/storage"
)

type WebhookHandler struct {
	cfg    config.Config
	openAI *service.OpenAIService
	gowa   *service.GOWAService
	meTube *service.MeTubeService
	memory *storage.PostgresConversationStore
	locker *storage.ChatLocker
	dedup  *storage.Deduplicator
	logger *log.Logger
}

func NewWebhookHandler(cfg config.Config, openAI *service.OpenAIService, gowa *service.GOWAService, meTube *service.MeTubeService, memory *storage.PostgresConversationStore, logger *log.Logger) *WebhookHandler {
	return &WebhookHandler{
		cfg:    cfg,
		openAI: openAI,
		gowa:   gowa,
		meTube: meTube,
		memory: memory,
		locker: storage.NewChatLocker(),
		dedup:  storage.NewDeduplicator(24 * time.Hour),
		logger: logger,
	}
}

func (h *WebhookHandler) Handle(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unable to read request body"})
		return
	}
	if !security.VerifyHMAC(body, c.GetHeader("X-Hub-Signature-256"), h.cfg.WebhookSecret) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	var event model.WebhookEvent
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
		return
	}
	if event.Event != "message" {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "reason": "unsupported event"})
		return
	}
	message := event.Payload
	if message.IsFromMe {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "reason": "message from self"})
		return
	}
	// Safety default: group messages are ignored before any read receipt,
	// typing indicator, OpenAI request, or outgoing reply.
	if !h.cfg.AllowGroupMessages && isGroup(message) {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "reason": "group messages disabled"})
		return
	}
	hasImage := h.cfg.ImageEnabled && message.Image.Present()
	// An image-only message is valid, so the body check must not reject it.
	if strings.TrimSpace(message.ChatID) == "" || (strings.TrimSpace(message.Body) == "" && !hasImage) {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "reason": "empty chat or message body"})
		return
	}

	dedupKey := event.DeviceID + ":" + message.ID
	if !h.dedup.First(dedupKey) {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "reason": "duplicate message"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
	go h.process(event)
}

func (h *WebhookHandler) process(event model.WebhookEvent) {
	releaseChat := h.locker.Acquire(event.Payload.ChatID)
	defer releaseChat()

	timeout := h.cfg.OpenAITimeout + h.cfg.GOWATimeout + h.cfg.GOWAMediaTimeout + 30*time.Second
	if h.cfg.MeTubeEnabled {
		timeout += h.cfg.MeTubeAddTimeout + h.cfg.MeTubeWaitTimeout + h.cfg.MeTubeRequestTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if h.cfg.HumanizeEnabled {
		if err := h.gowa.MarkRead(ctx, event.DeviceID, event.Payload.ChatID, event.Payload.ID); err != nil {
			h.logger.Printf("gowa mark read failed message_id=%s: %v", event.Payload.ID, err)
		} else {
			h.logger.Printf("gowa mark read ok message_id=%s", event.Payload.ID)
		}
		if err := h.gowa.SetChatPresence(ctx, event.DeviceID, event.Payload.ChatID, "start"); err != nil {
			h.logger.Printf("gowa typing start failed message_id=%s: %v", event.Payload.ID, err)
		} else {
			h.logger.Printf("gowa typing start ok message_id=%s chat_id=%s", event.Payload.ID, event.Payload.ChatID)
		}
	} else {
		h.logger.Printf("humanize disabled, skipping read/typing message_id=%s", event.Payload.ID)
	}

	// A media link is handled before the model so the file reaches the user as
	// fast as the download allows. The model is only consulted when the sender
	// also asked something beyond the link.
	if h.cfg.MeTubeEnabled {
		if hint, ok := service.ExtractLink(event.Payload.Body, h.cfg.MeTubeAllowedHosts, h.cfg.MeTubeAudioOnlyHosts); ok {
			handled, err := h.handleMediaLink(ctx, event, hint)
			if err != nil {
				h.logger.Printf("metube download failed message_id=%s: %v", event.Payload.ID, err)
			}
			// A failed download on a bare link still gets a short explanation
			// instead of silently dropping the message.
			if handled || hint.Silent {
				if !handled {
					notice := "Maaf, link itu gagal diunduh. Coba kirim ulang link-nya ya."
					if err != nil {
						h.logger.Printf("metube notice message_id=%s detail=%v", event.Payload.ID, err)
					}
					if sendErr := h.gowa.SendText(ctx, event.DeviceID, event.Payload.ChatID, notice); sendErr != nil {
						h.logger.Printf("gowa send notice failed message_id=%s: %v", event.Payload.ID, sendErr)
					}
				}
				if h.cfg.HumanizeEnabled {
					_ = h.gowa.SetChatPresence(ctx, event.DeviceID, event.Payload.ChatID, "stop")
				}
				return
			}
		}
	}

	startedTyping := time.Now()
	reply, err := h.askModel(ctx, event)
	if err != nil {
		if h.memory != nil {
			failedPrompt := strings.TrimSpace(event.Payload.Body)
			if failedPrompt == "" && event.Payload.Image.Present() {
				failedPrompt = "Deskripsikan gambar ini."
			}
			if failedPrompt != "" {
				if removeErr := h.memory.RemoveLastUser(ctx, event.Payload.ChatID, failedPrompt); removeErr != nil {
					h.logger.Printf("postgres remove failed user turn chat_id=%s: %v", event.Payload.ChatID, removeErr)
				}
			}
		}
		if h.cfg.HumanizeEnabled {
			_ = h.gowa.SetChatPresence(ctx, event.DeviceID, event.Payload.ChatID, "stop")
		}
		h.logger.Printf("openai failed message_id=%s chat_id=%s error=%v", event.Payload.ID, event.Payload.ChatID, err)
		return
	}

	if h.cfg.HumanizeEnabled {
		if remaining := h.cfg.HumanizeMinTypingTime - time.Since(startedTyping); remaining > 0 {
			select {
			case <-time.After(remaining):
			case <-ctx.Done():
				return
			}
		}
		if err := h.gowa.SetChatPresence(ctx, event.DeviceID, event.Payload.ChatID, "stop"); err != nil {
			h.logger.Printf("gowa typing stop failed message_id=%s: %v", event.Payload.ID, err)
		} else {
			h.logger.Printf("gowa typing stop ok message_id=%s", event.Payload.ID)
		}
	}

	if err := h.gowa.SendText(ctx, event.DeviceID, event.Payload.ChatID, reply); err != nil {
		h.logger.Printf("gowa send failed message_id=%s: %v", event.Payload.ID, err)
		return
	}
	if h.memory != nil && (strings.TrimSpace(event.Payload.Body) != "" || event.Payload.Image.Present()) {
		if memoryErr := h.memory.AddAssistant(ctx, event.Payload.ChatID, reply); memoryErr != nil {
			h.logger.Printf("postgres save assistant turn failed chat_id=%s: %v", event.Payload.ChatID, memoryErr)
		}
	}
	h.logger.Printf("message processed message_id=%s chat_id=%s response_sha256=%s", event.Payload.ID, event.Payload.ChatID, hash(reply))
}

// handleMediaLink queues the link on MeTube and forwards the finished file to
// WhatsApp. It reports whether the file was sent; a false result lets the
// caller fall back to a normal text reply.
func (h *WebhookHandler) handleMediaLink(ctx context.Context, event model.WebhookEvent, hint service.LinkHint) (bool, error) {
	media, err := h.meTube.Download(ctx, hint.Raw, hint.Audio)
	if err != nil {
		return false, err
	}
	if media.Audio {
		err = h.gowa.SendAudio(ctx, event.DeviceID, event.Payload.ChatID, media.FileURL, hint.VoiceNote)
	} else {
		err = h.gowa.SendVideo(ctx, event.DeviceID, event.Payload.ChatID, media.FileURL, media.Title, event.Payload.ID)
	}
	if err != nil {
		return false, err
	}
	h.logger.Printf("media sent message_id=%s chat_id=%s audio=%v url=%s", event.Payload.ID, event.Payload.ChatID, media.Audio, media.FileURL)
	return true, nil
}

func (h *WebhookHandler) askModel(ctx context.Context, event model.WebhookEvent) (string, error) {
	prompt := strings.TrimSpace(event.Payload.Body)
	if prompt == "" {
		prompt = "Deskripsikan gambar ini."
	}
	if !h.cfg.ImageEnabled || !event.Payload.Image.Present() {
		return h.replyWithMemory(ctx, event.Payload.ChatID, prompt)
	}

	downloadCtx, cancel := context.WithTimeout(ctx, h.cfg.GOWAMediaTimeout)
	defer cancel()
	data, mimeType, err := h.gowa.DownloadImage(downloadCtx, event.DeviceID, event.Payload.ChatID, event.Payload.ID)
	if err != nil {
		h.logger.Printf("gowa image download failed message_id=%s: %v", event.Payload.ID, err)
		return h.replyWithMemory(ctx, event.Payload.ChatID, prompt)
	}
	h.logger.Printf("gowa image ready message_id=%s bytes=%d mime=%s", event.Payload.ID, len(data), mimeType)
	if h.memory == nil {
		return h.openAI.ReplyWithImage(ctx, prompt, data, mimeType)
	}
	stored, err := h.memory.AddUserAndGetContext(ctx, event.Payload.ChatID, prompt)
	if err != nil {
		return "", fmt.Errorf("load conversation memory: %w", err)
	}
	history := make([]service.ConversationTurn, 0, len(stored)-1)
	for _, message := range stored[:len(stored)-1] {
		history = append(history, service.ConversationTurn{Role: message.Role, Content: message.Content})
	}
	return h.openAI.ReplyWithImageContext(ctx, prompt, data, mimeType, history)
}

func (h *WebhookHandler) replyWithMemory(ctx context.Context, chatID, prompt string) (string, error) {
	if h.memory == nil {
		return h.openAI.Reply(ctx, prompt)
	}
	stored, err := h.memory.AddUserAndGetContext(ctx, chatID, prompt)
	if err != nil {
		return "", fmt.Errorf("load conversation memory: %w", err)
	}
	history := make([]service.ConversationTurn, 0, len(stored))
	for _, message := range stored {
		history = append(history, service.ConversationTurn{Role: message.Role, Content: message.Content})
	}
	return h.openAI.ReplyWithContext(ctx, prompt, history)
}

func isGroup(message model.IncomingMessage) bool {
	chatID := strings.ToLower(strings.TrimSpace(message.ChatID))
	return message.IsGroup || strings.HasSuffix(chatID, "@g.us")
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
