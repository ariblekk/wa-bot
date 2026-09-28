package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Hosts that yt-dlp reliably supports. Keeping a default allow list means a
// stranger cannot turn the bot into an open proxy for arbitrary URLs. Override
// with METUBE_ALLOWED_HOSTS=* to accept everything.
const defaultMeTubeHosts = "youtube.com, youtu.be, m.youtube.com, music.youtube.com, " +
	"tiktok.com, vm.tiktok.com, vt.tiktok.com, instagram.com, instagr.am, facebook.com, fb.watch, " +
	"twitter.com, x.com, t.co, tiktokv.com, reddit.com, vimeo.com, dailymotion.com, " +
	"soundcloud.com, on.soundcloud.com, bilibili.com, b23.tv, snipd.com, tumblr.com, " +
	"streamable.com, vk.com, ok.ru, rumble.com, odysee.com, bandcamp.com, " +
	"mixcloud.com, audiomack.com, podcastaddict.com, artsandculture.google.com, " +
	"youtube-nocookie.com, likee.video, mixcloud.com"

// audioOnlyHosts never carry a video stream, so asking MeTube for video on them
// fails with "No video formats found". These default to the audio path unless
// the sender explicitly asks for video.
const defaultAudioOnlyHosts = "soundcloud.com, on.soundcloud.com, bandcamp.com, " +
	"mixcloud.com, audiomack.com, podcastaddict.com, snipd.com, archive.org"

type Config struct {
	AppHost               string
	AppPort               string
	AppEnv                string
	AppTimezone           *time.Location
	AppTimezoneName       string
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	ShutdownTimeout       time.Duration
	WebhookPath           string
	WebhookSecret         string
	OpenAIAPIKey          string
	OpenAIBaseURL         string
	OpenAIChatPath        string
	OpenAIChatURL         string
	OpenAIModel           string
	OpenAISystemPrompt    string
	OpenAIPromptFile      string
	OpenAITimeout         time.Duration
	OpenAIMaxTokens       int
	ChatMemoryEnabled     bool
	ChatMemoryMaxMessages int
	ChatMemoryTTL         time.Duration
	PostgresDSN           string
	PostgresMaxConns      int
	PostgresMinConns      int
	GOWABaseURL           string
	GOWASendPath          string
	GOWAReadMessagePath   string
	GOWAChatPresencePath  string
	GOWAAPIKey            string
	GOWABasicAuth         string
	GOWADeviceID          string
	GOWATimeout           time.Duration
	AllowGroupMessages    bool
	HumanizeEnabled       bool
	HumanizeMinTypingTime time.Duration
	Debug                 bool
	ImageEnabled          bool
	ImageMaxBytes         int
	GOWADownloadPath      string
	GOWAMediaTimeout      time.Duration
	GOWASendVideoPath     string
	GOWASendAudioPath     string
	MeTubeEnabled         bool
	MeTubeBaseURL         string
	MeTubeAPIKey          string
	MeTubeAddPath         string
	MeTubeHistoryPath     string
	MeTubeVideoFilePath   string
	MeTubeAudioFilePath   string
	MeTubeVideoQuality    string
	MeTubeVideoFormat     string
	MeTubeVideoCodec      string
	MeTubeAudioFormat     string
	MeTubeAllowedHosts    []string
	MeTubeAudioOnlyHosts  []string
	MeTubePollInterval    time.Duration
	MeTubeWaitTimeout     time.Duration
	MeTubeRequestTimeout  time.Duration
	MeTubeAddTimeout      time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		AppHost:              getEnv("APP_HOST", "0.0.0.0"),
		AppPort:              getEnv("APP_PORT", "8080"),
		AppEnv:               getEnv("APP_ENV", "development"),
		AppTimezoneName:      getEnv("APP_TIMEZONE", "Asia/Jakarta"),
		WebhookPath:          getEnv("WEBHOOK_PATH", "/webhook/gowa"),
		WebhookSecret:        os.Getenv("WEBHOOK_SECRET"),
		OpenAIAPIKey:         os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:        strings.TrimRight(getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		OpenAIChatPath:       getEnv("OPENAI_CHAT_COMPLETIONS_PATH", "/chat/completions"),
		OpenAIChatURL:        strings.TrimSpace(os.Getenv("OPENAI_CHAT_COMPLETIONS_URL")),
		OpenAIModel:          getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAISystemPrompt:   getEnv("OPENAI_SYSTEM_PROMPT", "Kamu adalah asisten WhatsApp yang ramah dan membantu."),
		OpenAIPromptFile:     strings.TrimSpace(os.Getenv("OPENAI_SYSTEM_PROMPT_FILE")),
		PostgresDSN:          strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		GOWABaseURL:          strings.TrimRight(getEnv("GOWA_BASE_URL", "http://localhost:3000"), "/"),
		GOWASendPath:         getEnv("GOWA_SEND_MESSAGE_PATH", "/send/message"),
		GOWAReadMessagePath:  getEnv("GOWA_READ_MESSAGE_PATH", "/message/{message_id}/read"),
		GOWAChatPresencePath: getEnv("GOWA_CHAT_PRESENCE_PATH", "/send/chat-presence"),
		GOWAAPIKey:           os.Getenv("GOWA_API_KEY"),
		GOWABasicAuth:        os.Getenv("GOWA_BASIC_AUTH"),
		GOWADeviceID:         os.Getenv("GOWA_DEVICE_ID"),
		GOWADownloadPath:     getEnv("GOWA_DOWNLOAD_MEDIA_PATH", "/message/{message_id}/download"),
		GOWASendVideoPath:    getEnv("GOWA_SEND_VIDEO_PATH", "/send/video"),
		GOWASendAudioPath:    getEnv("GOWA_SEND_AUDIO_PATH", "/send/audio"),
		MeTubeBaseURL:        strings.TrimRight(getEnv("METUBE_BASE_URL", ""), "/"),
		MeTubeAPIKey:         strings.TrimSpace(os.Getenv("METUBE_API_KEY")),
		MeTubeAddPath:        getEnv("METUBE_ADD_PATH", "/add"),
		MeTubeHistoryPath:    getEnv("METUBE_HISTORY_PATH", "/history"),
		MeTubeVideoFilePath:  getEnv("METUBE_VIDEO_FILE_PATH", "/download"),
		MeTubeAudioFilePath:  getEnv("METUBE_AUDIO_FILE_PATH", "/audio_download"),
		MeTubeVideoQuality:   getEnv("METUBE_VIDEO_QUALITY", "720"),
		MeTubeVideoFormat:    getEnv("METUBE_VIDEO_FORMAT", "mp4"),
		MeTubeVideoCodec:     getEnv("METUBE_VIDEO_CODEC", "h264"),
		MeTubeAudioFormat:    getEnv("METUBE_AUDIO_FORMAT", "mp3"),
		MeTubeAllowedHosts:   splitCSV(getEnv("METUBE_ALLOWED_HOSTS", defaultMeTubeHosts)),
		MeTubeAudioOnlyHosts: splitCSV(getEnv("METUBE_AUDIO_ONLY_HOSTS", defaultAudioOnlyHosts)),
	}

	var err error
	if cfg.AppTimezone, err = time.LoadLocation(cfg.AppTimezoneName); err != nil {
		return Config{}, fmt.Errorf("APP_TIMEZONE is invalid: %w", err)
	}
	if cfg.ReadTimeout, err = durationEnv("APP_READ_TIMEOUT_SECONDS", 15); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = durationEnv("APP_WRITE_TIMEOUT_SECONDS", 15); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("APP_SHUTDOWN_TIMEOUT_SECONDS", 10); err != nil {
		return Config{}, err
	}
	if cfg.OpenAITimeout, err = durationEnv("OPENAI_TIMEOUT_SECONDS", 60); err != nil {
		return Config{}, err
	}
	if cfg.GOWATimeout, err = durationEnv("GOWA_TIMEOUT_SECONDS", 30); err != nil {
		return Config{}, err
	}
	if cfg.OpenAIMaxTokens, err = intEnv("OPENAI_MAX_TOKENS", 0); err != nil {
		return Config{}, err
	}
	if cfg.ChatMemoryEnabled, err = boolEnv("CHAT_MEMORY_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.ChatMemoryMaxMessages, err = intEnv("CHAT_MEMORY_MAX_MESSAGES", 20); err != nil {
		return Config{}, err
	}
	if cfg.ChatMemoryTTL, err = durationEnv("CHAT_MEMORY_TTL_HOURS", 24); err != nil {
		return Config{}, err
	}
	if cfg.PostgresMaxConns, err = intEnv("POSTGRES_MAX_CONNS", 10); err != nil {
		return Config{}, err
	}
	if cfg.PostgresMinConns, err = intEnv("POSTGRES_MIN_CONNS", 2); err != nil {
		return Config{}, err
	}
	if cfg.PostgresMinConns > cfg.PostgresMaxConns {
		return Config{}, fmt.Errorf("POSTGRES_MIN_CONNS cannot exceed POSTGRES_MAX_CONNS")
	}
	if cfg.ChatMemoryEnabled && strings.TrimSpace(cfg.PostgresDSN) == "" {
		return Config{}, fmt.Errorf("POSTGRES_DSN is required when CHAT_MEMORY_ENABLED=true")
	}
	if cfg.AllowGroupMessages, err = boolEnv("ALLOW_GROUP_MESSAGES", false); err != nil {
		return Config{}, err
	}
	if cfg.HumanizeEnabled, err = boolEnv("HUMANIZE_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.HumanizeMinTypingTime, err = durationEnv("HUMANIZE_MIN_TYPING_SECONDS", 2); err != nil {
		return Config{}, err
	}
	if cfg.Debug, err = boolEnv("APP_DEBUG", false); err != nil {
		return Config{}, err
	}
	if cfg.ImageEnabled, err = boolEnv("IMAGE_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.ImageMaxBytes, err = intEnv("IMAGE_MAX_BYTES", 5*1024*1024); err != nil {
		return Config{}, err
	}
	if cfg.GOWAMediaTimeout, err = durationEnv("GOWA_MEDIA_TIMEOUT_SECONDS", 30); err != nil {
		return Config{}, err
	}
	if cfg.MeTubeEnabled, err = boolEnv("METUBE_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.MeTubePollInterval, err = durationEnv("METUBE_POLL_SECONDS", 3); err != nil {
		return Config{}, err
	}
	if cfg.MeTubeRequestTimeout, err = durationEnv("METUBE_REQUEST_TIMEOUT_SECONDS", 20); err != nil {
		return Config{}, err
	}
	// /add blocks while yt-dlp extracts metadata, which is far slower than the
	// /history poll, so it gets its own budget.
	if cfg.MeTubeAddTimeout, err = durationEnv("METUBE_ADD_TIMEOUT_SECONDS", 120); err != nil {
		return Config{}, err
	}
	if cfg.MeTubeWaitTimeout, err = durationEnv("METUBE_WAIT_TIMEOUT_SECONDS", 600); err != nil {
		return Config{}, err
	}

	// The feature stays off until a MeTube base URL is configured, so an empty
	// METUBE_BASE_URL can never break normal chat replies.
	cfg.MeTubeEnabled = cfg.MeTubeEnabled && cfg.MeTubeBaseURL != ""
	if cfg.MeTubeEnabled {
		if err := validateMeTube(cfg); err != nil {
			return Config{}, err
		}
	}

	// The prompt file is read once at startup, so response latency is
	// unaffected. OPENAI_SYSTEM_PROMPT is used as a fallback.
	if cfg.OpenAIPromptFile != "" {
		raw, err := os.ReadFile(cfg.OpenAIPromptFile)
		if err != nil {
			return Config{}, fmt.Errorf("read OPENAI_SYSTEM_PROMPT_FILE: %w", err)
		}
		prompt := strings.TrimSpace(string(raw))
		if prompt == "" {
			return Config{}, fmt.Errorf("OPENAI_SYSTEM_PROMPT_FILE is empty")
		}
		cfg.OpenAISystemPrompt = prompt
	}

	if cfg.WebhookPath == "" {
		return Config{}, fmt.Errorf("WEBHOOK_PATH cannot be empty")
	}
	if cfg.OpenAIAPIKey == "" {
		return Config{}, fmt.Errorf("OPENAI_API_KEY is required")
	}
	if cfg.OpenAIModel == "" {
		return Config{}, fmt.Errorf("OPENAI_MODEL cannot be empty")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback int) (time.Duration, error) {
	value := getEnv(key, strconv.Itoa(fallback))
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer (seconds)", key)
	}
	return time.Duration(seconds) * time.Second, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := getEnv(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := getEnv(key, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.ToLower(strings.TrimSpace(part)); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// MeTube's /add endpoint rejects unknown values, so the config is checked here
// instead of failing on every incoming link.
var (
	meTubeVideoQualities = map[string]bool{"best": true, "worst": true, "2160": true, "1440": true, "1080": true, "720": true, "480": true, "360": true, "240": true}
	meTubeVideoFormats   = map[string]bool{"any": true, "mp4": true, "ios": true}
	meTubeVideoCodecs    = map[string]bool{"auto": true, "h264": true, "h265": true, "av1": true, "vp9": true}
	meTubeAudioFormats   = map[string]bool{"auto": true, "m4a": true, "mp3": true, "opus": true, "wav": true, "flac": true}
)

func validateMeTube(cfg Config) error {
	if _, err := url.Parse(cfg.MeTubeBaseURL); err != nil {
		return fmt.Errorf("METUBE_BASE_URL is not a valid URL: %w", err)
	}
	if !strings.HasPrefix(cfg.MeTubeBaseURL, "http://") && !strings.HasPrefix(cfg.MeTubeBaseURL, "https://") {
		return fmt.Errorf("METUBE_BASE_URL must start with http:// or https://")
	}
	if !meTubeVideoQualities[strings.ToLower(cfg.MeTubeVideoQuality)] {
		return fmt.Errorf("METUBE_VIDEO_QUALITY must be one of best, worst, 2160, 1440, 1080, 720, 480, 360, 240")
	}
	if !meTubeVideoFormats[strings.ToLower(cfg.MeTubeVideoFormat)] {
		return fmt.Errorf("METUBE_VIDEO_FORMAT must be one of any, mp4, ios")
	}
	if !meTubeVideoCodecs[strings.ToLower(cfg.MeTubeVideoCodec)] {
		return fmt.Errorf("METUBE_VIDEO_CODEC must be one of auto, h264, h265, av1, vp9")
	}
	if !meTubeAudioFormats[strings.ToLower(cfg.MeTubeAudioFormat)] {
		return fmt.Errorf("METUBE_AUDIO_FORMAT must be one of auto, m4a, mp3, opus, wav, flac")
	}
	return nil
}
