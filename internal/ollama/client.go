package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ulf/paperless-aissist/internal/types"
)

// Client sends prompts to Ollama.
type Client struct {
	host  string
	model string
	http  *http.Client
}

func New(host, model string) *Client {
	return &Client{
		host:  strings.TrimRight(host, "/"),
		model: model,
		http:  &http.Client{},
	}
}

type generateRequest struct {
	Model     string          `json:"model"`
	Prompt    string          `json:"prompt"`
	Format    json.RawMessage `json:"format,omitempty"`
	Stream    bool            `json:"stream"`
	KeepAlive int             `json:"keep_alive"`             // -1 = keep loaded indefinitely
	Options   map[string]any  `json:"options,omitempty"`
}

type generateResponse struct {
	Response           string `json:"response"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
}

// Analyze sends document text + hints to Ollama and returns structured metadata and performance info.
func (c *Client) Analyze(prompt string) (*types.AnalysisResult, *types.OllamaPerf, error) {
	result, perf, err := c.generate(prompt)
	if err != nil {
		result, perf, err = c.generate(prompt)
		if err != nil {
			return nil, nil, err
		}
	}
	return result, perf, nil
}

func (c *Client) generate(prompt string) (*types.AnalysisResult, *types.OllamaPerf, error) {
	req := generateRequest{
		Model:     c.model,
		Prompt:    prompt,
		Format:    json.RawMessage(`"json"`),
		Stream:    false,
		KeepAlive: -1,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.http.Post(c.host+"/api/generate", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}
	var gen generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gen); err != nil {
		return nil, nil, err
	}
	var result types.AnalysisResult
	if err := json.Unmarshal([]byte(gen.Response), &result); err != nil {
		return nil, nil, fmt.Errorf("parse ollama response: %w (raw: %s)", err, gen.Response)
	}

	perf := buildPerf(gen)
	return &result, perf, nil
}

func buildPerf(gen generateResponse) *types.OllamaPerf {
	p := &types.OllamaPerf{
		TotalDurationMs: float64(gen.TotalDuration) / 1e6,
		LoadDurationMs:  float64(gen.LoadDuration) / 1e6,
		PromptTokens:    gen.PromptEvalCount,
		PromptEvalMs:    float64(gen.PromptEvalDuration) / 1e6,
		GeneratedTokens: gen.EvalCount,
		GeneratedMs:     float64(gen.EvalDuration) / 1e6,
	}
	if gen.EvalDuration > 0 {
		p.TokensPerSec = float64(gen.EvalCount) / (float64(gen.EvalDuration) / 1e9)
	}
	return p
}

type embedRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	KeepAlive int      `json:"keep_alive"` // -1 = keep loaded indefinitely
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed calls /api/embed and returns the embedding vector for one text.
func (c *Client) Embed(model, text string) ([]float32, error) {
	req := embedRequest{Model: model, Input: []string{text}, KeepAlive: -1}
	data, _ := json.Marshal(req)
	resp, err := c.http.Post(c.host+"/api/embed", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed error %d: %s", resp.StatusCode, string(body))
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return er.Embeddings[0], nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels returns the names of all models available in Ollama.
func (c *Client) ListModels() ([]string, error) {
	resp, err := c.http.Get(c.host + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tr tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	names := make([]string, len(tr.Models))
	for i, m := range tr.Models {
		names[i] = m.Name
	}
	return names, nil
}

// Generate sends a prompt and returns the raw text response (no JSON parsing).
// If model is empty, the client's default model is used.
func (c *Client) Generate(model, prompt string) (string, error) {
	if model == "" {
		model = c.model
	}
	req := generateRequest{
		Model:     model,
		Prompt:    prompt,
		Stream:    false,
		KeepAlive: -1,
	}
	data, _ := json.Marshal(req)
	resp, err := c.http.Post(c.host+"/api/generate", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}
	var gen generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gen); err != nil {
		return "", err
	}
	return gen.Response, nil
}

// ── /api/chat with tool calling ──────────────────────────────────────────────

// ChatTool describes a function the LLM may call.
type ChatTool struct {
	Type     string           `json:"type"`
	Function ChatToolFunction `json:"function"`
}

type ChatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatMessage is a single turn in a /api/chat conversation.
type ChatMessage struct {
	Role      string     `json:"role"`                 // system | user | assistant | tool
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is an LLM-requested tool invocation.
type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Tools     []ChatTool    `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
	KeepAlive int           `json:"keep_alive"` // -1 = keep loaded indefinitely
}

type chatResponse struct {
	Message            ChatMessage `json:"message"`
	EvalCount          int         `json:"eval_count"`
	EvalDuration       int64       `json:"eval_duration"`
	PromptEvalCount    int         `json:"prompt_eval_count"`
	TotalDuration      int64       `json:"total_duration"`
}

// ChatRoundPerf holds per-round LLM performance metrics.
type ChatRoundPerf struct {
	GeneratedTokens int
	TokensPerSec    float64
	TotalMs         float64
}

// Chat sends messages to /api/chat and returns the assistant reply and perf metrics.
// If the model requests tool calls they are returned in the message's ToolCalls field.
func (c *Client) Chat(model string, messages []ChatMessage, tools []ChatTool) (*ChatMessage, *ChatRoundPerf, error) {
	if model == "" {
		model = c.model
	}
	req := chatRequest{
		Model:     model,
		Messages:  messages,
		Tools:     tools,
		Stream:    false,
		KeepAlive: -1,
	}
	data, _ := json.Marshal(req)
	resp, err := c.http.Post(c.host+"/api/chat", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("ollama chat error %d: %s", resp.StatusCode, string(body))
	}
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, nil, err
	}
	perf := &ChatRoundPerf{
		GeneratedTokens: cr.EvalCount,
		TotalMs:         float64(cr.TotalDuration) / 1e6,
	}
	if cr.EvalDuration > 0 {
		perf.TokensPerSec = float64(cr.EvalCount) / (float64(cr.EvalDuration) / 1e9)
	}
	return &cr.Message, perf, nil
}

// BuildPrompt fills in the prompt template placeholders.
func BuildPrompt(template, docText string, hints types.Hints) string {
	p := template
	p = strings.ReplaceAll(p, "{{document}}", docText)
	p = strings.ReplaceAll(p, "{{tags}}", strings.Join(hints.Tags, ", "))
	p = strings.ReplaceAll(p, "{{correspondents}}", strings.Join(hints.Correspondents, ", "))
	p = strings.ReplaceAll(p, "{{document_types}}", strings.Join(hints.DocumentTypes, ", "))
	return p
}
