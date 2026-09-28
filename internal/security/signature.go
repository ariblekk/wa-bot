package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func VerifyHMAC(body []byte, signature, secret string) bool {
	if strings.TrimSpace(secret) == "" {
		return true
	}
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	received, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write(body)
	return hmac.Equal(received, hash.Sum(nil))
}
