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
	Model  string          `json:"model"`
	Prompt string          `json:"prompt"`
	Format json.RawMessage `json:"format,omitempty"`
	Stream bool            `json:"stream"`
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
		Model:  c.model,
		Prompt: prompt,
		Format: json.RawMessage(`"json"`),
		Stream: false,
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
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed calls /api/embed and returns the embedding vector for one text.
func (c *Client) Embed(model, text string) ([]float32, error) {
	req := embedRequest{Model: model, Input: []string{text}}
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
		Model:  model,
		Prompt: prompt,
		Stream: false,
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

// tagAnalysisSchema is a JSON Schema that enforces the structure of the tag analysis response.
// Ollama 0.5+ uses this to constrain token sampling, preventing type mismatches.
var tagAnalysisSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "tagId":     {"type": "integer"},
          "tagName":   {"type": "string"},
          "count":     {"type": "integer"},
          "action":    {"type": "string", "enum": ["keep", "merge", "delete"]},
          "mergeInto": {"type": "string"},
          "reason":    {"type": "string"}
        },
        "required": ["tagId", "tagName", "count", "action", "reason"]
      }
    },
    "summary": {"type": "string"}
  },
  "required": ["suggestions", "summary"]
}`)

// AnalyzeTags sends tags to Ollama for cleanup analysis
func (c *Client) AnalyzeTags(tags []types.TagWithCount) (*types.TagCleanupAnalysis, error) {
	analysis, err := c.analyzeTagsOnce(tags)
	if err != nil {
		analysis, err = c.analyzeTagsOnce(tags)
	}
	return analysis, err
}

func (c *Client) analyzeTagsOnce(tags []types.TagWithCount) (*types.TagCleanupAnalysis, error) {
	tagsJson, _ := json.MarshalIndent(tags, "", "  ")
	prompt := fmt.Sprintf(`You are a document tagging expert. Analyze these tags from a paperless-ngx document management system and suggest cleanups.

For each tag, decide if it should:
1. "keep" - useful and distinct
2. "merge" - merge into another existing tag (set mergeInto to the target tag name)
3. "delete" - unused or fully redundant

Every tag must appear exactly once in suggestions.
For "keep" and "delete" actions, set mergeInto to an empty string "".

Here are the tags (id, name, document count):
%s`, string(tagsJson))

	req := generateRequest{
		Model:  c.model,
		Prompt: prompt,
		Format: tagAnalysisSchema,
		Stream: false,
	}
	data, _ := json.Marshal(req)
	resp, err := c.http.Post(c.host+"/api/generate", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(body))
	}
	var gen generateResponse
	json.NewDecoder(resp.Body).Decode(&gen)

	var analysis types.TagCleanupAnalysis
	if err := json.Unmarshal([]byte(gen.Response), &analysis); err != nil {
		return nil, fmt.Errorf("parse ollama response: %w (raw: %s)", err, gen.Response)
	}
	return &analysis, nil
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
