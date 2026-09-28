package service

import (
	"strings"
	"testing"
	"time"

	"wa-chatbot/internal/config"
)

func TestSystemPromptIncludesCurrentServerTime(t *testing.T) {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	svc := NewOpenAIService(config.Config{
		OpenAISystemPrompt: "base prompt",
		AppTimezone:        location,
		AppTimezoneName:    "Asia/Jakarta",
	})

	prompt := svc.systemPrompt()
	for _, expected := range []string{
		"base prompt",
		"KONTEKS WAKTU SERVER",
		"Asia/Jakarta",
		now.Format("02 January 2006"),
		now.Format("15:04"),
		"Jangan mengatakan tidak bisa melihat waktu realtime",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("system prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}

func TestSystemPromptFallsBackToLocalTimezone(t *testing.T) {
	svc := NewOpenAIService(config.Config{OpenAISystemPrompt: "base"})
	prompt := svc.systemPrompt()
	if !strings.Contains(prompt, "KONTEKS WAKTU SERVER") {
		t.Fatal("expected time context even without explicit timezone")
	}
}
