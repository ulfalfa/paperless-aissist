package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/types"
)

// Client sends prompts to an OpenAI-compatible API (e.g. LiteLLM).
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

// ContextSize returns 0; OpenAI-compatible backends have no standard endpoint for this.
func (c *Client) ContextSize(_ string) int {
	return 0
}

// ─── shared OpenAI request/response types ────────────────────────────────────

type oaiMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []oaiTCCall  `json:"tool_calls,omitempty"`
}

type oaiTCCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiTCFunc   `json:"function"`
}

type oaiTCFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string in OpenAI format
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiToolDef  `json:"function"`
}

type oaiToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiResponseFormat struct {
	Type string `json:"type"`
}

type oaiChatRequest struct {
	Model          string             `json:"model"`
	Messages       []oaiMessage       `json:"messages"`
	Tools          []oaiTool          `json:"tools,omitempty"`
	Stream         bool               `json:"stream"`
	ResponseFormat *oaiResponseFormat `json:"response_format,omitempty"`
	Temperature    *float64           `json:"temperature,omitempty"`
	TopP           *float64           `json:"top_p,omitempty"`
}

type oaiChatResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) postChat(req oaiChatRequest) (*oaiChatResponse, time.Duration, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, 0, err
	}
	start := time.Now()
	resp, err := c.http.Post(c.host+"/v1/chat/completions", "application/json", bytes.NewReader(data))
	elapsed := time.Since(start)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(body))
	}
	var cr oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, 0, err
	}
	if len(cr.Choices) == 0 {
		return nil, 0, fmt.Errorf("openai: no choices in response")
	}
	return &cr, elapsed, nil
}

// ─── generate (document analysis with JSON output) ────────────────────────────

func (c *Client) Analyze(prompt string, opts types.ModelOptions) (*types.AnalysisResult, *types.OllamaPerf, error) {
	result, perf, err := c.generate(prompt, opts)
	if err != nil {
		result, perf, err = c.generate(prompt, opts)
		if err != nil {
			return nil, nil, err
		}
	}
	return result, perf, nil
}

func (c *Client) generate(prompt string, opts types.ModelOptions) (*types.AnalysisResult, *types.OllamaPerf, error) {
	model := c.model
	if opts.Model != "" {
		model = opts.Model
	}
	req := oaiChatRequest{
		Model: model,
		Messages: []oaiMessage{
			{Role: "user", Content: prompt},
		},
		Stream:         false,
		ResponseFormat: &oaiResponseFormat{Type: "json_object"},
	}
	if opts.Temperature > 0 {
		t := opts.Temperature
		req.Temperature = &t
	}
	if opts.TopP > 0 {
		p := opts.TopP
		req.TopP = &p
	}

	cr, elapsed, err := c.postChat(req)
	if err != nil {
		return nil, nil, err
	}

	content := cr.Choices[0].Message.Content
	var result types.AnalysisResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w (raw: %s)", err, content)
	}
	secs := elapsed.Seconds()
	perf := &types.OllamaPerf{
		TotalDurationMs: float64(elapsed.Milliseconds()),
		PromptTokens:    cr.Usage.PromptTokens,
		GeneratedTokens: cr.Usage.CompletionTokens,
	}
	if secs > 0 && cr.Usage.CompletionTokens > 0 {
		perf.TokensPerSec = float64(cr.Usage.CompletionTokens) / secs
	}
	return &result, perf, nil
}

// Generate sends a prompt and returns the raw text response.
func (c *Client) Generate(model, prompt string) (string, error) {
	if model == "" {
		model = c.model
	}
	cr, _, err := c.postChat(oaiChatRequest{
		Model:    model,
		Messages: []oaiMessage{{Role: "user", Content: prompt}},
		Stream:   false,
	})
	if err != nil {
		return "", err
	}
	return cr.Choices[0].Message.Content, nil
}

// ─── embeddings ──────────────────────────────────────────────────────────────

type oaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type oaiEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed calls /v1/embeddings and returns the embedding vector for one text.
func (c *Client) Embed(model, text string) ([]float32, error) {
	req := oaiEmbedRequest{Model: model, Input: []string{text}}
	data, _ := json.Marshal(req)
	resp, err := c.http.Post(c.host+"/v1/embeddings", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed error %d: %s", resp.StatusCode, string(body))
	}
	var er oaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return er.Data[0].Embedding, nil
}

// ─── model list ──────────────────────────────────────────────────────────────

type oaiModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels returns the names of all models available via /v1/models.
func (c *Client) ListModels() ([]string, error) {
	resp, err := c.http.Get(c.host + "/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var mr oaiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	names := make([]string, len(mr.Data))
	for i, m := range mr.Data {
		names[i] = m.ID
	}
	return names, nil
}

// ─── chat with tool calling ──────────────────────────────────────────────────

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

// ChatMessage is a single turn in a chat conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is an LLM-requested tool invocation.
type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatRoundPerf holds per-round LLM performance metrics.
type ChatRoundPerf struct {
	GeneratedTokens int
	PromptTokens    int
	TokensPerSec    float64
	TotalMs         float64
}

// Chat sends messages to /v1/chat/completions and returns the assistant reply.
func (c *Client) Chat(model string, messages []ChatMessage, tools []ChatTool, _ *bool) (*ChatMessage, *ChatRoundPerf, error) {
	if model == "" {
		model = c.model
	}

	oaiMsgs := make([]oaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = oaiMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			oaiMsgs[i].ToolCalls = append(oaiMsgs[i].ToolCalls, oaiTCCall{
				ID:   tc.ID,
				Type: "function",
				Function: oaiTCFunc{
					Name:      tc.Function.Name,
					Arguments: string(tc.Function.Arguments),
				},
			})
		}
	}

	oaiTools := make([]oaiTool, len(tools))
	for i, t := range tools {
		oaiTools[i] = oaiTool{
			Type: t.Type,
			Function: oaiToolDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}

	cr, elapsed, err := c.postChat(oaiChatRequest{
		Model:    model,
		Messages: oaiMsgs,
		Tools:    oaiTools,
		Stream:   false,
	})
	if err != nil {
		return nil, nil, err
	}

	raw := cr.Choices[0].Message
	msg := &ChatMessage{
		Role:    raw.Role,
		Content: raw.Content,
	}
	for _, tc := range raw.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		// Some backends double-encode arguments as a JSON string; un-quote if needed.
		if len(args) > 0 && args[0] == '"' {
			var s string
			if err := json.Unmarshal(args, &s); err == nil {
				args = json.RawMessage(s)
			}
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID: tc.ID,
			Function: ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}

	secs := elapsed.Seconds()
	perf := &ChatRoundPerf{
		PromptTokens:    cr.Usage.PromptTokens,
		GeneratedTokens: cr.Usage.CompletionTokens,
		TotalMs:         float64(elapsed.Milliseconds()),
	}
	if secs > 0 && cr.Usage.CompletionTokens > 0 {
		perf.TokensPerSec = float64(cr.Usage.CompletionTokens) / secs
	}
	return msg, perf, nil
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
