package service

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wa-chatbot/internal/config"
)

// newTestMeTube spins up a fake MeTube that finishes the queued download after
// the given number of history polls.
func newTestMeTube(t *testing.T, pollsBeforeDone int, history []string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload meTubeRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.URL == "" || payload.DownloadType == "" || payload.Quality == "" || payload.Format == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		n := int(atomic.AddInt32(&calls, 1))
		index := n - 1
		if index >= len(history) {
			index = len(history) - 1
		}
		_, _ = io.WriteString(w, history[index])
	})
	return server, &calls
}

func testConfig(base string) config.Config {
	return config.Config{
		MeTubeBaseURL:        base,
		MeTubeAddPath:        "/add",
		MeTubeHistoryPath:    "/history",
		MeTubeVideoFilePath:  "/download",
		MeTubeAudioFilePath:  "/audio_download",
		MeTubeVideoQuality:   "720",
		MeTubeVideoFormat:    "mp4",
		MeTubeVideoCodec:     "h264",
		MeTubeAudioFormat:    "mp3",
		MeTubeAllowedHosts:   []string{"youtube.com", "youtu.be"},
		MeTubePollInterval:   10 * time.Millisecond,
		MeTubeWaitTimeout:    5 * time.Second,
		MeTubeRequestTimeout: 5 * time.Second,
		MeTubeAddTimeout:     5 * time.Second,
		MeTubeEnabled:        true,
		Debug:                true,
	}
}

func quietLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestMeTubeDownloadVideoBuildsFileURL(t *testing.T) {
	pending := `{"done":[],"queue":[{"id":"abc","timestamp":1,"url":"https://youtu.be/abc","status":"downloading","title":"Sample","download_type":"video"}],"pending":[]}`
	finished := `{"done":[{"id":"abc","timestamp":1,"url":"https://youtu.be/abc","status":"finished","title":"Sample Video","filename":"Sample Video.mp4","download_type":"video"}],"queue":[],"pending":[]}`

	server, _ := newTestMeTube(t, 1, []string{pending, finished})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	media, err := svc.Download(context.Background(), "https://youtu.be/abc", false)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if media.Audio {
		t.Fatal("expected a video result")
	}
	if media.Title != "Sample Video" {
		t.Fatalf("unexpected title %q", media.Title)
	}
	want := server.URL + "/download/Sample%20Video.mp4"
	if media.FileURL != want {
		t.Fatalf("file url = %q, want %q", media.FileURL, want)
	}
}

func TestMeTubeDownloadAudioUsesAudioPath(t *testing.T) {
	finished := `{"done":[{"id":"abc","timestamp":2,"url":"https://youtu.be/abc","status":"finished","title":"Song","filename":"nested/Song.mp3","download_type":"audio"}],"queue":[],"pending":[]}`

	server, _ := newTestMeTube(t, 0, []string{finished})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	media, err := svc.Download(context.Background(), "https://youtu.be/abc", true)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !media.Audio {
		t.Fatal("expected an audio result")
	}
	want := server.URL + "/audio_download/nested/Song.mp3"
	if media.FileURL != want {
		t.Fatalf("file url = %q, want %q", media.FileURL, want)
	}
}

func TestMeTubeDownloadReusesQueuedEntry(t *testing.T) {
	// The URL is already queued before the add, so no new entry appears.
	queued := `{"done":[],"queue":[{"id":"abc","timestamp":7,"url":"https://youtu.be/abc?t=1","status":"downloading","download_type":"video"}],"pending":[]}`
	finished := `{"done":[{"id":"abc","timestamp":7,"url":"https://youtu.be/abc?t=1","status":"finished","title":"Reused","filename":"Reused.mp4","download_type":"video"}],"queue":[],"pending":[]}`

	server, _ := newTestMeTube(t, 1, []string{queued, finished})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	media, err := svc.Download(context.Background(), "https://youtu.be/abc", false)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !strings.HasSuffix(media.FileURL, "/download/Reused.mp4") {
		t.Fatalf("unexpected file url %q", media.FileURL)
	}
}

func TestMeTubeDownloadReportsError(t *testing.T) {
	failed := `{"done":[],"queue":[],"pending":[{"id":"abc","timestamp":9,"url":"https://youtu.be/abc","status":"error","msg":"Video unavailable"}]}`

	server, _ := newTestMeTube(t, 0, []string{failed})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	_, err := svc.Download(context.Background(), "https://youtu.be/abc", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Video unavailable") {
		t.Fatalf("error should carry the MeTube reason, got %v", err)
	}
}

func TestMeTubeAudioDoesNotReuseVideoEntry(t *testing.T) {
	// A video entry for the same URL already exists. Requesting the audio must
	// create its own entry instead of waiting on the video forever.
	queued := `{"done":[],"queue":[{"id":"abc","timestamp":7,"url":"https://youtu.be/abc","status":"downloading","download_type":"video"}],"pending":[]}`
	both := `{"done":[],"queue":[
		{"id":"abc","timestamp":7,"url":"https://youtu.be/abc","status":"downloading","download_type":"video"},
		{"id":"abc","timestamp":8,"url":"https://youtu.be/abc","status":"downloading","download_type":"audio"}
	],"pending":[]}`
	finished := `{"done":[
		{"id":"abc","timestamp":8,"url":"https://youtu.be/abc","status":"finished","title":"Tune","filename":"Tune.mp3","download_type":"audio"}
	],"queue":[],"pending":[]}`

	server, _ := newTestMeTube(t, 2, []string{queued, both, finished})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	media, err := svc.Download(context.Background(), "https://youtu.be/abc", true)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !strings.HasSuffix(media.FileURL, "/audio_download/Tune.mp3") {
		t.Fatalf("audio request returned %q, expected the mp3", media.FileURL)
	}
}

// A different YouTube video already in the queue must not be handed back when
// another video is requested.
func TestMeTubeDoesNotReuseDifferentVideo(t *testing.T) {
	otherDone := `{"done":[{"id":"zzz","timestamp":50,"url":"https://www.youtube.com/watch?v=other","status":"finished","title":"Other","filename":"Other.mp4","download_type":"video"}],"queue":[],"pending":[]}`
	wanted := `{"done":[{"id":"zzz","timestamp":50,"url":"https://www.youtube.com/watch?v=other","status":"finished","title":"Other","filename":"Other.mp4","download_type":"video"},{"id":"abc","timestamp":51,"url":"https://www.youtube.com/watch?v=wanted","status":"finished","title":"Wanted","filename":"Wanted.mp4","download_type":"video"}],"queue":[],"pending":[]}`

	server, _ := newTestMeTube(t, 1, []string{otherDone, wanted})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	media, err := svc.Download(context.Background(), "https://www.youtube.com/watch?v=wanted", false)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !strings.HasSuffix(media.FileURL, "/download/Wanted.mp4") {
		t.Fatalf("returned the wrong file: %s", media.FileURL)
	}
}

func TestMeTubeRejectsPrivateAndUnknownHosts(t *testing.T) {
	server, _ := newTestMeTube(t, 0, []string{`{"done":[],"queue":[],"pending":[]}`})
	svc := NewMeTubeService(testConfig(server.URL), quietLogger())

	for _, raw := range []string{
		"http://localhost:8081/x",
		"http://127.0.0.1/x",
		"http://192.168.1.5/x",
		"http://10.1.2.3/x",
		"https://evil.example.com/x",
		"file:///etc/passwd",
	} {
		if _, err := svc.Download(context.Background(), raw, false); err == nil {
			t.Fatalf("%s should have been rejected", raw)
		}
	}
}

func TestMeTubeDisabledShortCircuits(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1")
	cfg.MeTubeEnabled = false
	svc := NewMeTubeService(cfg, quietLogger())
	if _, err := svc.Download(context.Background(), "https://youtu.be/abc", false); err == nil {
		t.Fatal("a disabled integration must not call out")
	}
}

func TestMeTubeWaitTimesOut(t *testing.T) {
	forever := `{"done":[],"queue":[{"id":"abc","timestamp":3,"url":"https://youtu.be/abc","status":"downloading"}],"pending":[]}`
	server, _ := newTestMeTube(t, 100, []string{forever})
	cfg := testConfig(server.URL)
	cfg.MeTubeWaitTimeout = 60 * time.Millisecond
	svc := NewMeTubeService(cfg, quietLogger())

	if _, err := svc.Download(context.Background(), "https://youtu.be/abc", false); err == nil {
		t.Fatal("expected a timeout")
	}
}
