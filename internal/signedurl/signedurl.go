package signedurl

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	mu          sync.Mutex
	secretCache []byte
)

// GetSecret loads or generates a persistent HMAC secret stored in indexPath.
// Priority: FILE_URL_SECRET env var → indexPath/file_url_secret file → auto-generate.
func GetSecret(indexPath string) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if secretCache != nil {
		return secretCache, nil
	}
	if v := os.Getenv("FILE_URL_SECRET"); v != "" {
		if b, err := hex.DecodeString(v); err == nil && len(b) >= 32 {
			secretCache = b
			return secretCache, nil
		}
	}
	path := filepath.Join(indexPath, "file_url_secret")
	if data, err := os.ReadFile(path); err == nil {
		trimmed := strings.TrimSpace(string(data))
		if b, err := hex.DecodeString(trimmed); err == nil && len(b) >= 32 {
			secretCache = b
			return secretCache, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate file URL secret: %w", err)
	}
	// Best-effort persistence — ignore write errors (e.g. read-only data dir)
	_ = os.MkdirAll(indexPath, 0700)
	_ = os.WriteFile(path, []byte(hex.EncodeToString(b)), 0600)
	secretCache = b
	return secretCache, nil
}

// Sign creates a signed token for the given document ID and kind ("pdf" or "preview").
// Token format: <docID>.<kind>.<expireUnix>.<hmac-hex>
func Sign(docID int, kind string, ttl time.Duration, secret []byte) string {
	expire := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	payload := fmt.Sprintf("%d.%s.%s", docID, kind, expire)
	mac := computeHMAC(payload, secret)
	return payload + "." + mac
}

// Verify validates a signed token and returns the document ID and kind.
func Verify(token string, secret []byte) (docID int, kind string, err error) {
	parts := strings.SplitN(token, ".", 4)
	if len(parts) != 4 {
		return 0, "", fmt.Errorf("invalid token format")
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("invalid token")
	}
	k := parts[1]
	if k != "pdf" && k != "preview" {
		return 0, "", fmt.Errorf("invalid token kind")
	}
	expire, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid token")
	}
	if time.Now().Unix() > expire {
		return 0, "", fmt.Errorf("token expired")
	}
	payload := parts[0] + "." + parts[1] + "." + parts[2]
	expected := computeHMAC(payload, secret)
	if !hmac.Equal([]byte(parts[3]), []byte(expected)) {
		return 0, "", fmt.Errorf("invalid signature")
	}
	return id, k, nil
}

func computeHMAC(payload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
