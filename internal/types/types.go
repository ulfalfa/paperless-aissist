package types

// PaperlessDocument represents a document from paperless-ngx
type PaperlessDocument struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Created       string `json:"created"`
	Modified      string `json:"modified"`
	Correspondent *int   `json:"correspondent"`
	DocumentType  *int   `json:"document_type"`
	Tags          []int  `json:"tags"`
}

// PaperlessItem is a generic named item (tag, correspondent, document type)
type PaperlessItem struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	DocumentCount int    `json:"document_count"`
}

// Hints provides existing metadata context for the AI prompt
type Hints struct {
	Tags           []string
	Correspondents []string
	DocumentTypes  []string
}

// AnalysisResult is the structured output from the LLM
type AnalysisResult struct {
	Title         string   `json:"title"`
	Tags          []string `json:"tags"`
	CreatedDate   *string  `json:"created_date"`
	Correspondent *string  `json:"correspondent"`
	DocumentType  *string  `json:"document_type"`
}

// OpenAIPerf holds performance metrics from an OpenAI-compatible generation call.
type OpenAIPerf struct {
	TotalDurationMs    float64 `json:"totalDurationMs"`
	LoadDurationMs     float64 `json:"loadDurationMs"`
	PromptTokens       int     `json:"promptTokens"`
	PromptEvalMs       float64 `json:"promptEvalMs"`
	GeneratedTokens    int     `json:"generatedTokens"`
	GeneratedMs        float64 `json:"generatedMs"`
	TokensPerSec       float64 `json:"tokensPerSec"`
}

// DocumentUpdate is the payload sent to the paperless-ngx PATCH endpoint
type DocumentUpdate struct {
	Title         string `json:"title"`
	Created       string `json:"created"`
	Correspondent *int   `json:"correspondent"`
	DocumentType  *int   `json:"document_type"`
	Tags          []int  `json:"tags"`
}

// IndexStatus reports the state of the RAG indexer
type IndexStatus struct {
	Running        bool    `json:"running"`
	Total          int     `json:"total"`
	Done           int     `json:"done"`
	Errors         int     `json:"errors"`
	LastRun        *string `json:"lastRun"`
	DocCount       int     `json:"docCount"`
	PaperlessTotal int     `json:"paperlessTotal"`
	MissingCount   int     `json:"missingCount"`
}

// ChatSource is a document cited in a RAG answer
type ChatSource struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	ScorePercent int    `json:"scorePercent"`
	PaperlessURL string `json:"paperlessUrl"`
}

// ConversationMessage is a single turn in a chat conversation
type ConversationMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// ChatPerf holds performance metrics for a RAG query
type ChatPerf struct {
	TotalMs         float64 `json:"totalMs"`
	GeneratedTokens int     `json:"generatedTokens"`
	PromptTokens    int     `json:"promptTokens"`
	ContextSize     int     `json:"contextSize"`
	TokensPerSec    float64 `json:"tokensPerSec"`
	ToolCalls       int     `json:"toolCalls"`
	LLMRounds       int     `json:"llmRounds"`
}

// ChatResult is the response from a RAG query
type ChatResult struct {
	Answer  string                `json:"answer"`
	Sources []ChatSource          `json:"sources"`
	History []ConversationMessage `json:"history"`
	Perf    *ChatPerf             `json:"perf,omitempty"`
}

// LogEntry is a single log record
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // info | warn | error
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
}

// ScanStatus reports the current state of the auto-scanner
type ScanStatus struct {
	Running         bool     `json:"running"`
	LastRun         *string  `json:"lastRun"`
	LastCount       int      `json:"lastCount"`
	LastErrors      []string `json:"lastErrors"`
	NextRun         *string  `json:"nextRun"`
	IntervalMinutes int      `json:"intervalMinutes"`
	Total           int      `json:"total"`
	Done            int      `json:"done"`
}

// ModelOptions holds per-request LLM inference parameters.
// Zero values mean "use server default".
type ModelOptions struct {
	Model         string  `json:"model"`
	Temperature   float64 `json:"temperature"`
	TopK          int     `json:"topK"`
	TopP          float64 `json:"topP"`
	RepeatPenalty float64 `json:"repeatPenalty"`
	NumCtx        int     `json:"numCtx"`
	Think         bool    `json:"think"`
	UnloadAfter   bool    `json:"unloadAfter"` // if true, keep_alive=0 (unload immediately)
}

// AppConfig holds all runtime configuration
type AppConfig struct {
	OpenAIBaseURL  string `json:"openaiBaseUrl"`
	OpenAIModel    string `json:"openaiModel"`
	PromptFile     string `json:"promptFile"`
	PaperlessURL   string `json:"paperlessBaseUrl"`
	PaperlessToken string `json:"paperlessApiToken"`
	Port           string `json:"port"`
	ScanTag        string `json:"scanTag"`
	ScanInterval   int    `json:"scanInterval"`
	AITag          string `json:"aiTag"`
	StrictMetadata bool   `json:"strictMetadata"`
	EmbedModel     string `json:"embedModel"`
	IndexPath      string `json:"indexPath"`
	LogPrompts     bool   `json:"logPrompts"`
	// Model tuning (0 = use server default)
	Temperature   float64 `json:"temperature"`
	TopK          int     `json:"topK"`
	TopP          float64 `json:"topP"`
	RepeatPenalty float64 `json:"repeatPenalty"`
	NumCtx        int     `json:"numCtx"`
	Think         bool    `json:"think"`
	MCPToken      string  `json:"mcpToken"`
	OpenAIAPIKey  string  `json:"openaiApiKey"`
}
