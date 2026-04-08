package logger

import (
	"strings"
	"sync"
	"time"

	"github.com/ulf/paperless-aissist/internal/types"
)

const maxEntries = 200

var (
	mu      sync.Mutex
	entries []types.LogEntry
)

func add(level, message, detail string) {
	// Suppress noisy status polling logs
	if strings.Contains(message, "polling") || strings.Contains(message, "status check") {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	entry := types.LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   message,
		Detail:    detail,
	}
	entries = append([]types.LogEntry{entry}, entries...)
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
}

func Info(message string, detail ...string) {
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	add("info", message, d)
}

func Warn(message string, detail ...string) {
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	add("warn", message, d)
}

func Error(message string, detail ...string) {
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	add("error", message, d)
}

func All() []types.LogEntry {
	mu.Lock()
	defer mu.Unlock()
	out := make([]types.LogEntry, len(entries))
	copy(out, entries)
	return out
}

func Clear() {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
}
