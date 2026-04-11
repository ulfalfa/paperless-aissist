// Package promptlog writes full LLM prompt payloads to a JSONL file for
// inspection and learning purposes. Enabled via LOG_PROMPTS=true in .env.
package promptlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu      sync.Mutex
	logFile string
)

// Init sets the directory where prompts.jsonl will be written.
func Init(dir string) {
	logFile = filepath.Join(dir, "prompts.jsonl")
}

// Entry is one logged prompt round.
type Entry struct {
	Timestamp  string            `json:"ts"`
	Type       string            `json:"type"`                  // "llm" or "tool"
	Round      int               `json:"round"`
	DurationMs float64           `json:"duration_ms"`
	Meta       map[string]string `json:"meta,omitempty"`

	// LLM-specific
	Model      string  `json:"model,omitempty"`
	Messages   any     `json:"messages,omitempty"`
	Tools      any     `json:"tools,omitempty"`
	Response   any     `json:"response,omitempty"`
	Tokens     int     `json:"tokens,omitempty"`
	TokensPerS float64 `json:"tokens_per_s,omitempty"`

	// Tool-specific
	ToolName   string `json:"tool_name,omitempty"`
	ToolArgs   any    `json:"tool_args,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
	ToolError  string `json:"tool_error,omitempty"`
}

// WriteLLM logs one LLM round (called after oc.Chat returns).
func WriteLLM(round int, model string, messages, tools, response any, durationMs float64, tokens int, tokPerS float64, meta map[string]string) {
	write(Entry{
		Timestamp:  time.Now().Format(time.RFC3339),
		Type:       "llm",
		Round:      round,
		DurationMs: durationMs,
		Meta:       meta,
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		Response:   response,
		Tokens:     tokens,
		TokensPerS: tokPerS,
	})
}

// WriteTool logs one tool execution.
func WriteTool(round int, toolName string, args any, result string, toolErr string, durationMs float64) {
	write(Entry{
		Timestamp:  time.Now().Format(time.RFC3339),
		Type:       "tool",
		Round:      round,
		DurationMs: durationMs,
		ToolName:   toolName,
		ToolArgs:   args,
		ToolResult: result,
		ToolError:  toolErr,
	})
}

func write(entry Entry) {
	if logFile == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
}

// Clear truncates the log file.
func Clear() error {
	mu.Lock()
	defer mu.Unlock()
	return os.WriteFile(logFile, nil, 0644)
}

// Read returns the raw content of the log file.
func Read() ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	return os.ReadFile(logFile)
}

// Path returns the full path to the log file.
func Path() string { return logFile }
