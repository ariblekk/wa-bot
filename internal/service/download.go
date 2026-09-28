package service

import (
	"net/url"
	"regexp"
	"strings"
)

var linkPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"']+`)

// LinkHint is a supported media link plus what the surrounding words asked for.
type LinkHint struct {
	Raw       string
	Audio     bool
	VoiceNote bool
	Silent    bool
}

// ExtractLink pulls the first supported media link out of a message. Silent is
// true when the sender only wants the file, so no chatbot reply follows.
//
// audioOnlyHosts lets sites that publish no video stream default to audio, so a
// bare SoundCloud link is sent as an mp3 instead of failing.
func ExtractLink(text string, allowedHosts, audioOnlyHosts []string) (LinkHint, bool) {
	match := linkPattern.FindString(text)
	if match == "" {
		return LinkHint{}, false
	}
	raw := strings.TrimRight(match, ".,!?;:)]}'\"")
	if strings.HasPrefix(strings.ToLower(raw), "www.") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return LinkHint{}, false
	}
	if !hostAllowed(parsed.Hostname(), allowedHosts) {
		return LinkHint{}, false
	}

	surrounding := strings.TrimSpace(linkPattern.ReplaceAllString(text, " "))
	lower := strings.ToLower(surrounding)

	hint := LinkHint{Raw: raw}
	for _, keyword := range []string{"mp3", "musik", "music", "lagu", "audio", "sound", "only"} {
		if containsWord(lower, keyword) {
			hint.Audio = true
			break
		}
	}
	for _, keyword := range []string{"vn", "voice note", "suara", "ptt"} {
		if containsWord(lower, keyword) {
			hint.Audio = true
			hint.VoiceNote = true
			break
		}
	}
	// An explicit "video" or "vid" overrides the audio-only default, otherwise
	// the request would still fail on a site with no video stream.
	explicitVideo := containsWord(lower, "video") || containsWord(lower, "vid")
	if explicitVideo {
		hint.Audio = false
	}
	if !hint.Audio && !explicitVideo && hostAllowed(parsed.Hostname(), audioOnlyHosts) {
		hint.Audio = true
	}
	// A bare link, or a short "tolong download ini", means the file is the whole
	// request, so the model is not called at all.
	hint.Silent = surrounding == "" || isDownloadRequest(lower)
	return hint, true
}

func hostAllowed(host string, allowedHosts []string) bool {
	host = strings.ToLower(host)
	for _, allowed := range allowedHosts {
		if allowed == "*" {
			return true
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// fillerWords are words that can wrap a link without changing the intent, so
// "tolong download musiknya" is still treated as a plain download request.
var fillerWords = map[string]bool{
	"tolong": true, "dong": true, "please": true, "download": true, "unduh": true,
	"ambil": true, "take": true, "get": true, "video": true, "vid": true,
	"musik": true, "mp3": true, "audio": true, "lagu": true, "sound": true,
	"only": true, "vn": true, "voice": true, "note": true, "file": true,
	"ini": true, "itu": true, "yang": true, "yg": true, "the": true, "this": true,
	"dari": true, "di": true, "ke": true, "saya": true, "kamu": true, "me": true,
	"gua": true, "bro": true, "bang": true, "mas": true, "kak": true, "bisa": true,
	"sih": true, "ya": true, "yuk": true, "minta": true, "pengen": true, "mau": true,
}

// isDownloadRequest reports whether the text is only filler around a link.
func isDownloadRequest(text string) bool {
	words := strings.Fields(text)
	if len(words) == 0 {
		return true
	}
	for _, word := range words {
		cleaned := strings.Trim(word, ".,!?;:\"'")
		if fillerWords[cleaned] || fillerWords[trimSuffix(cleaned)] {
			continue
		}
		return false
	}
	return true
}

// trimSuffix removes the most common Indonesian clitic suffixes, longest first
// so "nya" is not shadowed by a shorter match.
func trimSuffix(word string) string {
	for _, suffix := range []string{"nya", "ku", "mu", "lah", "kah", "sih", "dong", "ya"} {
		if len(word) > len(suffix) && strings.HasSuffix(word, suffix) {
			return strings.TrimSuffix(word, suffix)
		}
	}
	return word
}

func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ',', '.', '!', '?', ':', ';', '"', '\'':
			return true
		}
		return false
	})
	for _, field := range fields {
		// A prefix match covers Indonesian suffixes the way people actually type
		// them: "musiknya", "mp3nya", "lagu-lagu".
		if strings.HasPrefix(field, word) {
			return true
		}
	}
	return false
}
