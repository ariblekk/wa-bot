package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadUsesConfiguredTimezone(t *testing.T) {
	keys := []string{
		"APP_TIMEZONE", "OPENAI_API_KEY", "OPENAI_MODEL", "METUBE_ENABLED", "METUBE_BASE_URL",
		"CHAT_MEMORY_ENABLED", "POSTGRES_DSN",
	}
	old := make(map[string]string, len(keys))
	for _, key := range keys {
		old[key], _ = os.LookupEnv(key)
	}
	t.Cleanup(func() {
		for _, key := range keys {
			if value, ok := old[key]; ok {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})

	_ = os.Setenv("APP_TIMEZONE", "Asia/Jakarta")
	_ = os.Setenv("OPENAI_API_KEY", "test-key")
	_ = os.Setenv("OPENAI_MODEL", "test-model")
	_ = os.Setenv("METUBE_ENABLED", "false")
	_ = os.Setenv("METUBE_BASE_URL", "")
	_ = os.Setenv("CHAT_MEMORY_ENABLED", "false")
	_ = os.Setenv("POSTGRES_DSN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.AppTimezone == nil {
		t.Fatal("expected AppTimezone to be loaded")
	}
	if cfg.AppTimezoneName != "Asia/Jakarta" {
		t.Fatalf("timezone name = %q", cfg.AppTimezoneName)
	}
	if got := cfg.AppTimezone.String(); got != "Asia/Jakarta" {
		t.Fatalf("timezone = %q", got)
	}
}

func TestLoadRejectsInvalidTimezone(t *testing.T) {
	oldTimezone, hadTimezone := os.LookupEnv("APP_TIMEZONE")
	oldKey, hadKey := os.LookupEnv("OPENAI_API_KEY")
	oldModel, hadModel := os.LookupEnv("OPENAI_MODEL")
	oldEnabled, hadEnabled := os.LookupEnv("METUBE_ENABLED")
	oldBase, hadBase := os.LookupEnv("METUBE_BASE_URL")
	t.Cleanup(func() {
		restoreEnv("APP_TIMEZONE", oldTimezone, hadTimezone)
		restoreEnv("OPENAI_API_KEY", oldKey, hadKey)
		restoreEnv("OPENAI_MODEL", oldModel, hadModel)
		restoreEnv("METUBE_ENABLED", oldEnabled, hadEnabled)
		restoreEnv("METUBE_BASE_URL", oldBase, hadBase)
	})

	_ = os.Setenv("APP_TIMEZONE", "Not/A/Timezone")
	_ = os.Setenv("OPENAI_API_KEY", "test-key")
	_ = os.Setenv("OPENAI_MODEL", "test-model")
	_ = os.Setenv("METUBE_ENABLED", "false")
	_ = os.Setenv("METUBE_BASE_URL", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_TIMEZONE") {
		t.Fatalf("expected invalid timezone error, got %v", err)
	}
}

func restoreEnv(key, value string, existed bool) {
	if existed {
		_ = os.Setenv(key, value)
	} else {
		_ = os.Unsetenv(key)
	}
}
