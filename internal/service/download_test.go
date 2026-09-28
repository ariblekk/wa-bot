package service

import "testing"

var testHosts = []string{"youtube.com", "youtu.be", "tiktok.com", "soundcloud.com", "instagram.com"}

var testAudioOnly = []string{"soundcloud.com", "bandcamp.com"}

func TestExtractLinkBareLink(t *testing.T) {
	hint, ok := ExtractLink("https://youtu.be/dQw4w9WgXcQ", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected the link to be detected")
	}
	if hint.Raw != "https://youtu.be/dQw4w9WgXcQ" {
		t.Fatalf("unexpected raw url %q", hint.Raw)
	}
	if !hint.Silent {
		t.Fatal("a bare link should be silent")
	}
	if hint.Audio {
		t.Fatal("a bare link should default to video")
	}
}

func TestExtractLinkAudioRequest(t *testing.T) {
	hint, ok := ExtractLink("tolong download musiknya https://youtu.be/abc123", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected the link to be detected")
	}
	if !hint.Audio {
		t.Fatal("musik should switch to audio")
	}
	if !hint.Silent {
		t.Fatal("a short download request should be silent")
	}
}

func TestExtractLinkWithQuestionKeepsModel(t *testing.T) {
	hint, ok := ExtractLink("who is the speaker in this clip? https://youtu.be/xyz789", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected the link to be detected")
	}
	if hint.Silent {
		t.Fatal("a real question must still get a text answer")
	}
}

func TestExtractLinkAudioOnlyHostDefaultsToAudio(t *testing.T) {
	hint, ok := ExtractLink("https://soundcloud.com/artist/track", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected SoundCloud to be detected")
	}
	if !hint.Audio {
		t.Fatal("audio-only hosts should default to audio")
	}
}

func TestExtractLinkExplicitVideoOverridesAudioOnlyHost(t *testing.T) {
	hint, ok := ExtractLink("download video https://soundcloud.com/artist/track", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected SoundCloud to be detected")
	}
	if hint.Audio {
		t.Fatal("explicit video should override the audio-only default")
	}
}

func TestExtractLinkInstagramAndTikTok(t *testing.T) {
	for _, raw := range []string{
		"https://www.instagram.com/reel/ABC123/",
		"https://www.tiktok.com/@creator/video/123456",
	} {
		if _, ok := ExtractLink(raw, []string{"instagram.com", "tiktok.com"}, nil); !ok {
			t.Fatalf("expected supported link %q to be detected", raw)
		}
	}
}

func TestExtractLinkDisallowedHost(t *testing.T) {
	if _, ok := ExtractLink("https://example.com/video.mp4", testHosts, testAudioOnly); ok {
		t.Fatal("a host outside the allow list must be ignored")
	}
}

func TestExtractLinkTrailingPunctuation(t *testing.T) {
	hint, ok := ExtractLink("lihat ini https://youtu.be/abc.", testHosts, testAudioOnly)
	if !ok {
		t.Fatal("expected the link to be detected")
	}
	if hint.Raw != "https://youtu.be/abc" {
		t.Fatalf("trailing dot should be trimmed, got %q", hint.Raw)
	}
}

func TestExtractLinkSubdomain(t *testing.T) {
	if _, ok := ExtractLink("https://m.youtube.com/watch?v=abc", testHosts, testAudioOnly); !ok {
		t.Fatal("a subdomain of an allowed host should be accepted")
	}
}

func TestIsPrivateHost(t *testing.T) {
	private := []string{"localhost", "10.0.0.5", "127.0.0.1", "192.168.1.10", "172.16.4.4", "169.254.1.1", "nas.local"}
	for _, host := range private {
		if !isPrivateHost(host) {
			t.Fatalf("%s should be treated as private", host)
		}
	}
	public := []string{"youtube.com", "8.8.8.8", "172.32.0.1", "11.0.0.1"}
	for _, host := range public {
		if isPrivateHost(host) {
			t.Fatalf("%s should not be treated as private", host)
		}
	}
}

func TestSameTargetIgnoresTrackingQuery(t *testing.T) {
	if !sameTarget("https://youtu.be/abc?t=10", "https://youtu.be/abc") {
		t.Fatal("a tracking parameter should not break matching")
	}
	if sameTarget("https://youtu.be/other", "https://youtu.be/abc") {
		t.Fatal("different ids must not match")
	}
}

// A YouTube watch link carries the video id in ?v=, so two different videos on
// the same path must never look like the same target.
func TestSameTargetKeepsYouTubeVideoID(t *testing.T) {
	a := "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	b := "https://www.youtube.com/watch?v=aqz-KE-bpKQ"
	if sameTarget(a, b) {
		t.Fatalf("distinct YouTube videos must not match: %s vs %s", a, b)
	}
	if !sameTarget(a, "https://www.youtube.com/watch?app=desktop&v=jNQXAC9IVRw&t=30s") {
		t.Fatal("the same YouTube video with extra params should still match")
	}
}

func TestCanonicalURLNormalizesHostAndCase(t *testing.T) {
	if canonicalURL("https://WWW.YouTube.com/watch/") != canonicalURL("https://youtube.com/watch") {
		t.Fatal("host case and www prefix should not create a new identity")
	}
}
