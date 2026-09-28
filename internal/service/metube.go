package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wa-chatbot/internal/config"
)

// MeTubeService talks to a self-hosted MeTube instance (https://github.com/alexta69/metube).
// The bot only ever enqueues a download and polls the queue; the resulting file is
// pulled by GoWA directly from the MeTube URL, so nothing is stored in this project.
type MeTubeService struct {
	cfg       config.Config
	client    *http.Client
	addClient *http.Client
	logger    *log.Logger
}

func NewMeTubeService(cfg config.Config, logger *log.Logger) *MeTubeService {
	return &MeTubeService{
		cfg: cfg,
		// /history is a cheap queue read, /add waits on yt-dlp metadata
		// extraction, so they get separate timeouts.
		client:    &http.Client{Timeout: cfg.MeTubeRequestTimeout},
		addClient: &http.Client{Timeout: cfg.MeTubeAddTimeout},
		logger:    logger,
	}
}

type meTubeRequest struct {
	URL          string `json:"url"`
	DownloadType string `json:"download_type"`
	Quality      string `json:"quality"`
	Format       string `json:"format"`
	Codec        string `json:"codec"`
	AutoStart    bool   `json:"auto_start"`
}

type meTubeResult struct {
	Status string `json:"status"`
	Msg    string `json:"msg"`
	Error  string `json:"error"`
}

// meTubeItem mirrors the fields MeTube exposes in GET /history.
type meTubeItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	Filename     string `json:"filename"`
	DownloadType string `json:"download_type"`
	Timestamp    int64  `json:"timestamp"`
	Error        string `json:"error"`
	Msg          string `json:"msg"`
}

// key identifies one queue entry. MeTube reuses the yt-dlp entry id for the
// same video across kinds, so the timestamp is what makes each add unique.
func (i meTubeItem) key() string {
	return i.ID + "@" + strconv.FormatInt(i.Timestamp, 10)
}

type meTubeHistory struct {
	Done    []meTubeItem `json:"done"`
	Queue   []meTubeItem `json:"queue"`
	Pending []meTubeItem `json:"pending"`
}

func (h meTubeHistory) all() []meTubeItem {
	items := make([]meTubeItem, 0, len(h.Done)+len(h.Queue)+len(h.Pending))
	items = append(items, h.Done...)
	items = append(items, h.Queue...)
	items = append(items, h.Pending...)
	return items
}

// MeTubeMedia is the finished download, ready to be handed to GoWA.
type MeTubeMedia struct {
	Title   string
	FileURL string
	Audio   bool
}

type mediaKind int

const (
	kindVideo mediaKind = iota
	kindAudio
)

// Download enqueues the URL on MeTube and blocks until the download reaches a
// terminal state, then returns the public URL GoWA can fetch.
func (s *MeTubeService) Download(ctx context.Context, rawURL string, audio bool) (MeTubeMedia, error) {
	if !s.cfg.MeTubeEnabled {
		return MeTubeMedia{}, errors.New("MeTube integration is disabled")
	}
	if err := s.validateURL(rawURL); err != nil {
		return MeTubeMedia{}, err
	}

	// Short links such as vt.tiktok.com/... are expanded by yt-dlp/MeTube
	// before they appear in /history. Use the expanded URL only for matching;
	// keep the original URL for /add so MeTube handles the download itself.
	matchURL := s.resolveShortURL(ctx, rawURL)

	kind := kindVideo
	if audio {
		kind = kindAudio
	}

	payload := meTubeRequest{
		URL:          rawURL,
		DownloadType: "video",
		Quality:      strings.ToLower(s.cfg.MeTubeVideoQuality),
		Format:       strings.ToLower(s.cfg.MeTubeVideoFormat),
		Codec:        strings.ToLower(s.cfg.MeTubeVideoCodec),
		AutoStart:    true,
	}
	if kind == kindAudio {
		payload = meTubeRequest{
			URL:          rawURL,
			DownloadType: "audio",
			Quality:      "best",
			Format:       strings.ToLower(s.cfg.MeTubeAudioFormat),
			AutoStart:    true,
		}
	}

	// MeTube's /add response carries no id, so the queue is captured first and
	// the new entry is identified by diffing afterwards.
	before, err := s.history(ctx)
	if err != nil {
		return MeTubeMedia{}, fmt.Errorf("read MeTube history: %w", err)
	}
	known := make(map[string]bool, len(before.all()))
	for _, item := range before.all() {
		known[item.key()] = true
	}
	// MeTube keeps one entry per URL and download type, so re-adding a link that
	// is still in flight returns "already in queue" and creates no new entry.
	// That entry is reused instead of waiting for one that will never appear.
	// The type has to match, otherwise asking for the audio of a link that is
	// already being fetched as video would hand back the video.
	// An entry is reusable only when it is the same kind, or when MeTube did
	// not record a kind at all. An entry explicitly filed as the other kind is
	// skipped, so asking for the audio of a link that is already downloading as
	// video waits for its own entry instead of returning the video.
	var existing string
	for _, item := range before.all() {
		if !sameTargetAny(item.URL, rawURL, matchURL) {
			continue
		}
		if sameKind(item.DownloadType, kind) {
			existing = item.key()
			break
		}
	}

	if err := s.add(ctx, payload); err != nil {
		return MeTubeMedia{}, err
	}

	item, err := s.wait(ctx, rawURL, matchURL, kind, known, existing)
	if err != nil {
		return MeTubeMedia{}, err
	}

	fileURL, err := s.fileURL(item)
	if err != nil {
		return MeTubeMedia{}, err
	}
	if s.cfg.Debug {
		s.logger.Printf("metube ready id=%s status=%s url=%s", item.ID, item.Status, fileURL)
	}
	return MeTubeMedia{Title: item.Title, FileURL: fileURL, Audio: kind == kindAudio}, nil
}

func (s *MeTubeService) add(ctx context.Context, payload meTubeRequest) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode MeTube request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(s.cfg.MeTubeAddPath), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create MeTube request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.MeTubeAPIKey != "" {
		req.Header.Set("X-API-Key", s.cfg.MeTubeAPIKey)
	}

	resp, err := s.addClient.Do(req)
	if err != nil {
		return fmt.Errorf("call MeTube add: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read MeTube add response: %w", err)
	}
	if s.cfg.Debug {
		s.logger.Printf("metube call path=%s status=%d response=%s", s.cfg.MeTubeAddPath, resp.StatusCode, truncate(string(body), 300))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MeTube add returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var result meTubeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode MeTube add response: %w", err)
	}
	if result.Status == "error" {
		reason := result.Msg
		if reason == "" {
			reason = result.Error
		}
		if reason == "" {
			reason = result.Status
		}
		return fmt.Errorf("MeTube refused the download: %s", reason)
	}
	return nil
}

func (s *MeTubeService) history(ctx context.Context) (meTubeHistory, error) {
	var history meTubeHistory
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint(s.cfg.MeTubeHistoryPath), nil)
	if err != nil {
		return history, fmt.Errorf("create MeTube history request: %w", err)
	}
	if s.cfg.MeTubeAPIKey != "" {
		req.Header.Set("X-API-Key", s.cfg.MeTubeAPIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return history, fmt.Errorf("call MeTube history: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return history, fmt.Errorf("read MeTube history: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return history, fmt.Errorf("MeTube history returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	if err := json.Unmarshal(body, &history); err != nil {
		return history, fmt.Errorf("decode MeTube history: %w", err)
	}
	return history, nil
}

// sameKind reports whether a queue entry belongs to the requested media kind.
// MeTube keeps video and audio entries for one URL side by side under the same
// yt-dlp id, so matching on the URL alone can return the wrong file. An entry
// with no recorded kind is accepted, since it cannot be the other kind.
func sameKind(downloadType string, kind mediaKind) bool {
	recorded := strings.ToLower(strings.TrimSpace(downloadType))
	if recorded == "" {
		return true
	}
	if kind == kindAudio {
		return recorded == "audio"
	}
	return recorded != "audio"
}

// wait polls the queue until the newly added entry finishes or fails.
func (s *MeTubeService) wait(ctx context.Context, target, matchTarget string, kind mediaKind, known map[string]bool, existing string) (meTubeItem, error) {
	deadline := time.Now().Add(s.cfg.MeTubeWaitTimeout)
	ticker := time.NewTicker(s.cfg.MeTubePollInterval)
	defer ticker.Stop()

	var found *meTubeItem
	for {
		history, err := s.history(ctx)
		if err != nil {
			return meTubeItem{}, err
		}
		if found == nil {
			if existing != "" {
				for _, item := range history.all() {
					if item.key() == existing {
						found = &item
						break
					}
				}
			}
			for _, item := range history.all() {
				if found != nil {
					break
				}
				if !known[item.key()] && sameTargetAny(item.URL, target, matchTarget) && sameKind(item.DownloadType, kind) {
					found = &item
				}
			}
			if found == nil && s.cfg.Debug {
				s.logger.Printf("metube waiting for new queue entry url=%s", target)
			}
		}
		if found != nil {
			for _, item := range history.all() {
				if item.key() == found.key() {
					*found = item
					break
				}
			}
			switch found.Status {
			case "finished":
				return *found, nil
			case "error":
				reason := found.Msg
				if reason == "" {
					reason = found.Error
				}
				if reason == "" {
					reason = "unknown error"
				}
				return meTubeItem{}, fmt.Errorf("MeTube download failed: %s", reason)
			}
		}
		if time.Now().After(deadline) {
			return meTubeItem{}, fmt.Errorf("MeTube download timed out after %s", s.cfg.MeTubeWaitTimeout)
		}
		select {
		case <-ctx.Done():
			return meTubeItem{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// fileURL builds the static URL GoWA will download. MeTube serves finished
// video from /download and audio from /audio_download.
func (s *MeTubeService) fileURL(item meTubeItem) (string, error) {
	name := strings.TrimSpace(item.Filename)
	if name == "" {
		return "", errors.New("MeTube finished without a file name")
	}
	base := s.cfg.MeTubeVideoFilePath
	if strings.EqualFold(item.DownloadType, "audio") {
		base = s.cfg.MeTubeAudioFilePath
	}
	segments := strings.Split(strings.TrimLeft(name, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return s.endpoint(strings.TrimRight(base, "/") + "/" + strings.Join(segments, "/")), nil
}

// validateURL keeps the bot from being used as a general purpose downloader
// for internal or private network targets.
func (s *MeTubeService) validateURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https links are supported")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return errors.New("link has no host")
	}
	if isPrivateHost(host) {
		return errors.New("private and local addresses are not allowed")
	}
	if len(s.cfg.MeTubeAllowedHosts) == 0 {
		return errors.New("no allowed hosts configured")
	}
	for _, allowed := range s.cfg.MeTubeAllowedHosts {
		if allowed == "*" {
			return nil
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}
	return fmt.Errorf("host %s is not on the allowed list", host)
}

func (s *MeTubeService) endpoint(path string) string {
	return strings.TrimRight(s.cfg.MeTubeBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// sameTarget matches a queue entry against the requested link.
func sameTarget(entryURL, target string) bool {
	return canonicalURL(entryURL) == canonicalURL(target)
}

func sameTargetAny(entryURL string, targets ...string) bool {
	for _, target := range targets {
		if strings.TrimSpace(target) != "" && sameTarget(entryURL, target) {
			return true
		}
	}
	return false
}

// resolveShortURL follows redirects for known short-link hosts. TikTok's vt.tiktok.com
// redirect is the important case here; MeTube stores the expanded www.tiktok.com
// URL in history, so matching the original short URL alone would wait forever.
func (s *MeTubeService) resolveShortURL(ctx context.Context, rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !isShortLinkHost(parsed.Hostname()) {
		return rawURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return rawURL
	}
	resp, err := s.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if location := resp.Request.URL.String(); location != "" {
			return location
		}
	}

	// Some short-link servers reject HEAD. A GET with redirect following is the
	// fallback; no response body is retained.
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return rawURL
	}
	resp, err = s.client.Do(req)
	if err != nil {
		return rawURL
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	_ = resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return rawURL
}

func isShortLinkHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	return host == "vt.tiktok.com" || host == "vm.tiktok.com" || host == "t.co" ||
		host == "youtu.be" || host == "fb.watch" || host == "bit.ly" || host == "tinyurl.com"
}

// identityParams are the query parameters that decide which media a link points
// at. YouTube in particular carries the video id in ?v=, so the query string
// must never be dropped wholesale.
var identityParams = []string{"v", "list", "id"}

// canonicalURL reduces a link to a stable identity. Dropping the query outright
// would collapse every youtube.com/watch?v=... link onto the same key and make
// the bot send the wrong file, so only the parameters that identify the media
// are kept.
func canonicalURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(trimmed)
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	key := host + strings.TrimSuffix(parsed.Path, "/")

	// Most sites carry the media identity in the path and only tracking noise in
	// the query, so the query is dropped unless it holds an identity parameter.
	query := parsed.Query()
	identity := url.Values{}
	for _, name := range identityParams {
		if values, ok := query[name]; ok {
			for _, value := range values {
				identity.Add(name, value)
			}
		}
	}
	if encoded := identity.Encode(); encoded != "" {
		key += "?" + encoded
	}
	return key
}

func isPrivateHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	labels := strings.Split(host, ".")
	if len(labels) == 4 {
		first, err1 := parseOctet(labels[0])
		second, err2 := parseOctet(labels[1])
		if err1 == nil && err2 == nil {
			switch {
			case first == 10, first == 127:
				return true
			case first == 172 && second >= 16 && second <= 31:
				return true
			case first == 192 && second == 168:
				return true
			case first == 169 && second == 254:
				return true
			}
		}
	}
	return false
}

func parseOctet(value string) (int, error) {
	if value == "" || len(value) > 3 {
		return 0, errors.New("not a number")
	}
	var n int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	if n > 255 {
		return 0, errors.New("out of range")
	}
	return n, nil
}
