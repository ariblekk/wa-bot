package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMeTube hits the real MeTube instance. It only runs when
// METUBE_LIVE_TEST=1 and METUBE_BASE_URL is set.
func TestLiveMeTube(t *testing.T) {
	if os.Getenv("METUBE_LIVE_TEST") != "1" {
		t.Skip("set METUBE_LIVE_TEST=1 to run against a real MeTube instance")
	}
	base := os.Getenv("METUBE_BASE_URL")
	if base == "" {
		t.Skip("METUBE_BASE_URL is not set")
	}

	cfg := testConfig(base)
	cfg.MeTubeAllowedHosts = []string{"*"}
	cfg.MeTubeWaitTimeout = 10 * time.Minute
	cfg.MeTubeAddTimeout = 2 * time.Minute
	svc := NewMeTubeService(cfg, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Two different videos, so a mix-up in URL matching is visible.
	const firstVideo = "https://www.youtube.com/watch?v=aqz-KE-bpKQ"
	const secondVideo = "https://www.youtube.com/watch?v=jNQXAC9IVRw"

	audio, err := svc.Download(ctx, firstVideo, true)
	if err != nil {
		t.Fatalf("live audio download failed: %v", err)
	}
	t.Logf("audio title=%q url=%s", audio.Title, audio.FileURL)
	if !strings.Contains(audio.FileURL, "/audio_download/") {
		t.Fatalf("audio should come from the audio path, got %s", audio.FileURL)
	}

	video, err := svc.Download(ctx, secondVideo, false)
	if err != nil {
		t.Fatalf("live video download failed: %v", err)
	}
	t.Logf("video title=%q url=%s", video.Title, video.FileURL)
	if !strings.Contains(video.FileURL, "/download/") {
		t.Fatalf("video should come from the video path, got %s", video.FileURL)
	}
	if audio.FileURL == video.FileURL {
		t.Fatal("two different videos resolved to the same file")
	}

	tiktok, err := svc.Download(ctx, "https://vt.tiktok.com/ZSbMTNjPc/", false)
	if err != nil {
		t.Fatalf("live TikTok short-link download failed: %v", err)
	}
	t.Logf("TikTok title=%q url=%s", tiktok.Title, tiktok.FileURL)
	if !strings.Contains(tiktok.FileURL, "/download/") {
		t.Fatalf("TikTok should resolve to the video path, got %s", tiktok.FileURL)
	}
}
