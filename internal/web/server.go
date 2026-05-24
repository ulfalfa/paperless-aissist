package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"html/template"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	assets "github.com/ulf/paperless-aissist/web"
	"github.com/ulf/paperless-aissist/internal/auth"
	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/mcp"
	"github.com/ulf/paperless-aissist/internal/promptlog"
	"github.com/ulf/paperless-aissist/internal/openai"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/scanner"
	"github.com/ulf/paperless-aissist/internal/types"
	"github.com/ulf/paperless-aissist/internal/validate"
)

var authManager *auth.Manager
var appVersion = "dev"

func SetVersion(v string) { appVersion = v }

// ── Chat job store ────────────────────────────────────────────────────────────

type chatJob struct {
	mu       sync.Mutex
	status   string
	done     bool
	question string
	model    string
	data     map[string]any
}

var chatJobs sync.Map // jobID (string) → *chatJob

func newJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	tmplFunc template.FuncMap
	partials *template.Template
)

func init() {
	tmplFunc = template.FuncMap{
		"deref": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
		"divf": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"percent": func(a, b int) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b) * 100
		},
		"derefInt": func(i *int) int {
			if i == nil {
				return 0
			}
			return *i
		},
		"toJSON": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"join": strings.Join,
		"map": func(pairs ...any) map[string]any {
			m := make(map[string]any)
			for i := 0; i+1 < len(pairs); i += 2 {
				if key, ok := pairs[i].(string); ok {
					m[key] = pairs[i+1]
				}
			}
			return m
		},
		"slice": func(s string, a, b int) string {
			if a > len(s) {
				return ""
			}
			if b > len(s) {
				b = len(s)
			}
			return s[a:b]
		},
		"not": func(v any) bool {
			if v == nil {
				return true
			}
			switch val := v.(type) {
			case bool:
				return !val
			case string:
				return val == ""
			case int:
				return val == 0
			case []types.LogEntry:
				return len(val) == 0
			}
			return false
		},
		"len": func(v any) int {
			if v == nil {
				return 0
			}
			rv := reflect.ValueOf(v)
			switch rv.Kind() {
			case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
				return rv.Len()
			}
			return 0
		},
	}

	// Partials don't define "content" so they can share one template set
	partials = template.Must(template.New("").Funcs(tmplFunc).ParseFS(assets.FS,
		"templates/save_form.html",
		"templates/analyze_result.html",
		"templates/tag_docs.html",
		"templates/scan_status.html",
		"templates/settings_form.html",
		"templates/prompt_form.html",
		"templates/logs_partial.html",
		"templates/index_status.html",
		"templates/chat_answer.html",
		"templates/chat_loading.html",
		"templates/prompt_entries.html",
		"templates/test_result.html",
	))
}

// pageTmpl loads layout + all partials + a specific page file into an isolated
// template set. Including only one page file avoids {{define "content"}} conflicts.
func pageTmpl(page string) *template.Template {
	return template.Must(template.New("").Funcs(tmplFunc).ParseFS(assets.FS,
		"templates/layout.html",
		"templates/save_form.html",
		"templates/analyze_result.html",
		"templates/tag_docs.html",
		"templates/scan_status.html",
		"templates/settings_form.html",
		"templates/prompt_form.html",
		"templates/logs_partial.html",
		"templates/index_status.html",
		"templates/chat_answer.html",
		"templates/chat_loading.html",
		"templates/prompt_entries.html",
		"templates/test_result.html",
		"templates/"+page,
	))
}

func NewRouter(am *auth.Manager, mcpToken string) http.Handler {
	authManager = am

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(am.Middleware)

	// Auth routes (public)
	r.Get("/setup", handleSetupPage)
	r.Post("/setup", handleSetupForm)
	r.Get("/login", handleLoginPage)
	r.Post("/login", handleLoginForm)
	r.Post("/logout", handleLogout)

	// MCP endpoint — only mounted when MCP_API_TOKEN is configured
	if mcpToken != "" {
		mcpHandler := mcp.NewHTTPHandler()
		tokenGuard := auth.TokenMiddleware(mcpToken)
		r.With(tokenGuard).Handle("/mcp", mcpHandler)
		r.With(tokenGuard).Handle("/mcp/*", mcpHandler)
	}

	// API
	r.Get("/api/config", handleGetConfig)
	r.Post("/api/config", handlePostConfig)
	r.Get("/api/prompt", handleGetPrompt)
	r.Post("/api/prompt", handlePostPrompt)
	r.Post("/api/analyze", handleAnalyze)
	r.Post("/api/webhook", handleWebhook)
	r.Patch("/api/documents/{id}", handleUpdateDocument)
	r.Get("/api/metadata", handleGetMetadata)
	r.Get("/api/documents", handleListDocuments)
	r.Get("/api/scan/status", handleScanStatus)
	r.Post("/api/scan/run", handleScanRun)
	r.Get("/api/logs", handleGetLogs)
	r.Delete("/api/logs", handleClearLogs)
	r.Get("/api/prompts", handleGetPromptLog)
	r.Delete("/api/prompts", handleClearPromptLog)

	// UI pages
	r.Get("/test", handleTestPage)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/chat", http.StatusFound)
	})
	r.Get("/analyze", handleAnalyzePage)
	r.Get("/scan", handleScanPage)
	r.Get("/chat", handleChatPage)
	r.Get("/settings", handleSettingsPage)
	r.Get("/logs", handleLogsPage)
	r.Get("/prompts", handlePromptsPage)

	// HTMX partials
	r.Post("/ui/analyze", handleAnalyzeForm)
	r.Post("/ui/documents/{id}/save", handleSaveDocument)
	r.Post("/ui/scan/run", handleScanRunForm)
	r.Get("/ui/scan/status", handleScanStatusPartial)
	r.Get("/ui/logs", handleLogsPartial)
	r.Get("/ui/tags/docs", handleTagDocsPartial)
	r.Post("/ui/settings", handleSettingsForm)
	r.Post("/ui/prompt", handlePromptForm)
	r.Post("/ui/chat", handleChatForm)
	r.Get("/ui/chat/status/{jobID}", handleChatStatus)
	r.Post("/ui/rag/index", handleIndexRun)
	r.Get("/ui/rag/status", handleIndexStatus)
	r.Post("/ui/test/compare", handleTestCompare)
	r.Post("/ui/test/apply-variant", handleApplyVariant)

	return r
}

// ── Template rendering helpers ──────────────────────────────────────────────

type pageData struct {
	ActivePage string
	Data       any
	Flash      string
	Error      string
	Version    string
}

func render(w http.ResponseWriter, page string, d pageData) {
	d.Version = appVersion
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl(page).ExecuteTemplate(w, "layout.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func renderPartial(w http.ResponseWriter, tmpl string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := partials.ExecuteTemplate(w, tmpl, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ── API handlers ─────────────────────────────────────────────────────────────

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, cfg)
}

func handlePostConfig(w http.ResponseWriter, r *http.Request) {
	var cfg types.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := config.Save(&cfg); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	scanner.Stop()
	if cfg.ScanInterval > 0 {
		scanner.Start(cfg.ScanInterval)
	}
	jsonResponse(w, map[string]bool{"ok": true})
}

func handleGetPrompt(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	content, err := config.LoadPrompt(cfg.PromptFile)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, map[string]string{"content": content})
}

func handlePostPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	cfg, _ := config.Load()
	if err := config.SavePrompt(cfg.PromptFile, body.Content); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, map[string]bool{"ok": true})
}

type analyzeResponse struct {
	Document       *types.PaperlessDocument `json:"document"`
	Analysis       *types.AnalysisResult    `json:"analysis"`
	Perf           *types.OpenAIPerf        `json:"perf"`
	Tags           []types.PaperlessItem    `json:"tags"`
	Correspondents []types.PaperlessItem    `json:"correspondents"`
	DocumentTypes  []types.PaperlessItem    `json:"documentTypes"`
	EffectiveModel string                   `json:"effectiveModel"`
}

func analyzeDocument(id int, override *types.ModelOptions) (*analyzeResponse, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	// Start from config defaults, then apply per-call overrides
	opts := types.ModelOptions{
		Temperature:   cfg.Temperature,
		TopK:          cfg.TopK,
		TopP:          cfg.TopP,
		RepeatPenalty: cfg.RepeatPenalty,
		NumCtx:        cfg.NumCtx,
		Think:         cfg.Think,
	}
	if override != nil {
		if override.Model != "" {
			opts.Model = override.Model
		}
		if override.Temperature > 0 {
			opts.Temperature = override.Temperature
		}
		if override.TopK > 0 {
			opts.TopK = override.TopK
		}
		if override.TopP > 0 {
			opts.TopP = override.TopP
		}
		if override.RepeatPenalty > 0 {
			opts.RepeatPenalty = override.RepeatPenalty
		}
		if override.NumCtx > 0 {
			opts.NumCtx = override.NumCtx
		}
		if override.Think {
			opts.Think = true
		}
		opts.UnloadAfter = override.UnloadAfter
	}

	effectiveModel := cfg.OpenAIModel
	if opts.Model != "" {
		effectiveModel = opts.Model
	}

	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)

	doc, err := pc.GetDocument(id)
	if err != nil {
		return nil, fmt.Errorf("fetch document: %w", err)
	}
	promptTemplate, err := config.LoadPrompt(cfg.PromptFile)
	if err != nil {
		return nil, fmt.Errorf("load prompt: %w", err)
	}
	tags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()

	hints := types.Hints{
		Tags:           namesOf(tags),
		Correspondents: namesOf(correspondents),
		DocumentTypes:  namesOf(docTypes),
	}
	prompt := openai.BuildPrompt(promptTemplate, doc.Content, hints)
	result, perf, err := oc.Analyze(prompt, opts)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	result = validate.Apply(result, cfg.AITag)
	logger.Info(fmt.Sprintf("Analyzed doc %d: %s (%.1fs, %.0f tok/s)",
		id, result.Title, perf.TotalDurationMs/1000, perf.TokensPerSec))

	return &analyzeResponse{
		Document:       doc,
		Analysis:       result,
		Perf:           perf,
		Tags:           tags,
		Correspondents: correspondents,
		DocumentTypes:  docTypes,
		EffectiveModel: effectiveModel,
	}, nil
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DocumentID int `json:"documentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	resp, err := analyzeDocument(body.DocumentID, nil)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, resp)
}

func handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonError(w, 400, "invalid id")
		return
	}
	var result types.AnalysisResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

	doc, err := pc.GetDocument(id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	tags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()

	var tagIDs []int
	for _, name := range result.Tags {
		tid, err := pc.ResolveOrCreate("tag", name, tags)
		if err == nil && tid != nil {
			tagIDs = append(tagIDs, *tid)
		}
	}
	var corrID *int
	if result.Correspondent != nil {
		corrID, _ = pc.ResolveOrCreate("correspondent", *result.Correspondent, correspondents)
	}
	var dtID *int
	if result.DocumentType != nil {
		dtID, _ = pc.ResolveOrCreate("document_type", *result.DocumentType, docTypes)
	}
	created := doc.Created
	if result.CreatedDate != nil {
		created = *result.CreatedDate + "T00:00:00Z"
	}
	update := types.DocumentUpdate{
		Title:         result.Title,
		Created:       created,
		Correspondent: corrID,
		DocumentType:  dtID,
		Tags:          tagIDs,
	}
	if err := pc.UpdateDocument(id, update); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	logger.Info(fmt.Sprintf("Updated doc %d: %s", id, result.Title))
	jsonResponse(w, map[string]bool{"ok": true})
}

// handleWebhook is called by paperless-ngx when a document is consumed/updated.
// Auth: Authorization: Token <PAPERLESS_API_TOKEN>
// Body: {"document_id": 123}
func handleWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		jsonError(w, 500, "config error")
		return
	}

	// Token auth — same format as paperless-ngx API
	if r.Header.Get("Authorization") != "Token "+cfg.PaperlessToken {
		w.Header().Set("WWW-Authenticate", `Token realm="paperless-aissist"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rawBody, _ := io.ReadAll(r.Body)
	logger.Info(fmt.Sprintf("Webhook: POST from %s", r.RemoteAddr), string(rawBody))

	var body struct {
		DocumentID int `json:"document_id"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil || body.DocumentID == 0 {
		jsonError(w, 400, "missing document_id")
		return
	}

	logger.Info(fmt.Sprintf("Webhook: analyzing document %d", body.DocumentID))

	resp, err := analyzeDocument(body.DocumentID, nil)
	if err != nil {
		logger.Error(fmt.Sprintf("Webhook: analysis failed for doc %d", body.DocumentID), err.Error())
		jsonError(w, 500, err.Error())
		return
	}

	// Save analysis result to paperless-ngx
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	result := resp.Analysis
	var tagIDs []int
	for _, name := range result.Tags {
		tid, err := pc.ResolveOrCreate("tag", name, resp.Tags)
		if err == nil && tid != nil {
			tagIDs = append(tagIDs, *tid)
		}
	}
	var corrID *int
	if result.Correspondent != nil {
		corrID, _ = pc.ResolveOrCreate("correspondent", *result.Correspondent, resp.Correspondents)
	}
	var dtID *int
	if result.DocumentType != nil {
		dtID, _ = pc.ResolveOrCreate("document_type", *result.DocumentType, resp.DocumentTypes)
	}
	created := resp.Document.Created
	if result.CreatedDate != nil {
		created = *result.CreatedDate + "T00:00:00Z"
	}
	update := types.DocumentUpdate{
		Title:         result.Title,
		Created:       created,
		Correspondent: corrID,
		DocumentType:  dtID,
		Tags:          tagIDs,
	}
	if err := pc.UpdateDocument(body.DocumentID, update); err != nil {
		logger.Error(fmt.Sprintf("Webhook: update failed for doc %d", body.DocumentID), err.Error())
		jsonError(w, 500, err.Error())
		return
	}
	logger.Info(fmt.Sprintf("Webhook: saved doc %d: %s", body.DocumentID, result.Title))

	// Update RAG index in background
	go rag.Global().RunOnce(false)

	jsonResponse(w, map[string]any{"ok": true, "title": result.Title})
}

func handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	tags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()
	jsonResponse(w, map[string]any{
		"tags":           tags,
		"correspondents": correspondents,
		"documentTypes":  docTypes,
	})
}

func handleListDocuments(w http.ResponseWriter, r *http.Request) {
	tagIDStr := r.URL.Query().Get("tagId")
	tagID, err := strconv.Atoi(tagIDStr)
	if err != nil {
		jsonError(w, 400, "invalid tagId")
		return
	}
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	docs, err := pc.ListDocumentsByTag(tagID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	type docSummary struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Created string `json:"created"`
	}
	var out []docSummary
	for _, d := range docs {
		out = append(out, docSummary{ID: d.ID, Title: d.Title, Created: d.Created})
	}
	jsonResponse(w, out)
}

func handleScanStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, scanner.GetStatus())
}

func handleScanRun(w http.ResponseWriter, r *http.Request) {
	if scanner.IsRunning() {
		jsonError(w, 409, "scan already running")
		return
	}
	go scanner.RunOnce()
	jsonResponse(w, map[string]bool{"ok": true})
}

func handleGetLogs(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, logger.All())
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	logger.Clear()
	jsonResponse(w, map[string]bool{"ok": true})
}

func handleGetPromptLog(w http.ResponseWriter, r *http.Request) {
	data, err := promptlog.Read()
	if err != nil {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="prompts.jsonl"`)
	w.Write(data)
}

func handleClearPromptLog(w http.ResponseWriter, r *http.Request) {
	if err := promptlog.Clear(); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Return empty entries partial for HTMX swap
	renderPartial(w, "prompt_entries.html", promptPageData())
}

// ── Prompt log page ───────────────────────────────────────────────────────────

type promptEntryView struct {
	Type       string
	Timestamp  string
	Round      int
	DurationMs float64
	Meta       map[string]string

	// LLM fields
	Model      string
	Messages   []promptMessageView
	Response   *promptMessageView
	Tools      []promptToolView
	ToolCount  int
	Tokens     int
	TokensPerS float64

	// Tool fields
	ToolName   string
	ToolArgs   string
	ToolResult string
	ToolError  string

	// UI
	Question     string // last user message (for summary display)
	HistoryCount int    // number of prior turns (messages minus system + latest user)
	RawRequest   string // full JSON payload sent to the LLM
	IsLast       bool
}

type promptMessageView struct {
	Role      string
	Content   string
	ToolCalls []promptToolCallView
}

type promptToolCallView struct {
	Name string
	Args string
}

type promptToolView struct {
	Name        string
	Description string
}

type promptsData struct {
	LogPath string
	Entries []promptEntryView
}

func promptPageData() promptsData {
	path := promptlog.Path()
	if path == "" {
		return promptsData{}
	}
	raw, err := promptlog.Read()
	if err != nil || len(raw) == 0 {
		return promptsData{LogPath: path}
	}

	var entries []promptEntryView
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e promptlog.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}

		// Base fields for all entries
		entry := promptEntryView{
			Type:       e.Type,
			Timestamp:  e.Timestamp,
			Round:      e.Round,
			DurationMs: e.DurationMs,
			Meta:       e.Meta,
		}

		if e.Type == "llm" {
			// Parse messages
			rawMsgs, _ := json.Marshal(e.Messages)
			var msgs []struct {
				Role      string            `json:"role"`
				Content   string            `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			}
			json.Unmarshal(rawMsgs, &msgs)

			var msgViews []promptMessageView
			for _, m := range msgs {
				mv := promptMessageView{Role: m.Role, Content: m.Content}
				for _, tc := range m.ToolCalls {
					var parsed struct {
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					}
					if json.Unmarshal(tc, &parsed) == nil {
						args, _ := json.MarshalIndent(parsed.Function.Arguments, "", "  ")
						mv.ToolCalls = append(mv.ToolCalls, promptToolCallView{
							Name: parsed.Function.Name,
							Args: string(args),
						})
					}
				}
				msgViews = append(msgViews, mv)
			}

			// Parse response
			rawResp, _ := json.Marshal(e.Response)
			var respMsg struct {
				Role      string            `json:"role"`
				Content   string            `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			}
			if json.Unmarshal(rawResp, &respMsg) == nil && respMsg.Role != "" {
				respView := &promptMessageView{Role: respMsg.Role, Content: respMsg.Content}
				for _, tc := range respMsg.ToolCalls {
					var parsed struct {
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					}
					if json.Unmarshal(tc, &parsed) == nil {
						args, _ := json.MarshalIndent(parsed.Function.Arguments, "", "  ")
						respView.ToolCalls = append(respView.ToolCalls, promptToolCallView{
							Name: parsed.Function.Name,
							Args: string(args),
						})
					}
				}
				entry.Response = respView
			}

			// Parse tools
			rawTools, _ := json.Marshal(e.Tools)
			var tools []struct {
				Function struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"function"`
			}
			json.Unmarshal(rawTools, &tools)

			var toolViews []promptToolView
			for _, t := range tools {
				toolViews = append(toolViews, promptToolView{
					Name:        t.Function.Name,
					Description: t.Function.Description,
				})
			}

			// Extract last user message as question for summary display
			for i := len(msgViews) - 1; i >= 0; i-- {
				if msgViews[i].Role == "user" {
					q := msgViews[i].Content
					if len(q) > 80 {
						q = q[:80] + "…"
					}
					entry.Question = q
					break
				}
			}
			// History = all messages except system (first) and latest user (last)
			if len(msgViews) > 2 {
				entry.HistoryCount = len(msgViews) - 2
			}

			// Build raw JSON request payload as sent to the LLM
			rawPayload, _ := json.MarshalIndent(map[string]any{
				"model":    e.Model,
				"messages": e.Messages,
				"tools":    e.Tools,
				"stream":   false,
			}, "", "  ")
			entry.RawRequest = string(rawPayload)

			entry.Model = e.Model
			entry.Messages = msgViews
			entry.Tools = toolViews
			entry.ToolCount = len(toolViews)
			entry.Tokens = e.Tokens
			entry.TokensPerS = e.TokensPerS
		} else if e.Type == "tool" {
			// Parse tool args
			rawArgs, _ := json.Marshal(e.ToolArgs)
			args, _ := json.MarshalIndent(rawArgs, "", "  ")

			entry.ToolName = e.ToolName
			entry.ToolArgs = string(args)
			entry.ToolResult = e.ToolResult
			entry.ToolError = e.ToolError
		}

		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		entries[len(entries)-1].IsLast = true
	}

	return promptsData{LogPath: path, Entries: entries}
}

func handlePromptsPage(w http.ResponseWriter, r *http.Request) {
	render(w, "prompts.html", pageData{
		ActivePage: "prompts",
		Data:       promptPageData(),
	})
}

// ── UI page handlers ──────────────────────────────────────────────────────────

func handleAnalyzePage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	tags, _ := pc.ListTags()

	// Pre-select SCAN_TAG if configured
	var scanTagID *int
	var taggedDocs []types.PaperlessDocument
	for _, t := range tags {
		if strings.EqualFold(t.Name, cfg.ScanTag) {
			id := t.ID
			scanTagID = &id
			taggedDocs, _ = pc.ListDocumentsByTag(id)
			break
		}
	}
	render(w, "analyze.html", pageData{
		ActivePage: "analyze",
		Data: map[string]any{
			"Tags":       tags,
			"ScanTag":    cfg.ScanTag,
			"ScanTagID":  scanTagID,
			"TaggedDocs": taggedDocs,
		},
	})
}

func handleTagDocsPartial(w http.ResponseWriter, r *http.Request) {
	tagIDStr := r.URL.Query().Get("tagId")
	if tagIDStr == "" {
		renderPartial(w, "tag_docs.html", nil)
		return
	}
	tagID, err := strconv.Atoi(tagIDStr)
	if err != nil {
		renderPartial(w, "tag_docs.html", nil)
		return
	}
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	docs, err := pc.ListDocumentsByTag(tagID)
	if err != nil {
		renderPartial(w, "tag_docs.html", nil)
		return
	}
	renderPartial(w, "tag_docs.html", docs)
}

func handleScanPage(w http.ResponseWriter, r *http.Request) {
	render(w, "scan.html", pageData{
		ActivePage: "scan",
		Data: map[string]any{
			"ScanStatus":  scanner.GetStatus(),
			"IndexStatus": enrichedIndexStatus(),
		},
	})
}

func fetchModels(host, apiKey string, current ...string) []string {
	oc := openai.New(host, "", apiKey)
	models, _ := oc.ListModels()
	for _, cur := range current {
		if cur == "" {
			continue
		}
		found := false
		for _, m := range models {
			if m == cur {
				found = true
				break
			}
		}
		if !found {
			models = append([]string{cur}, models...)
		}
	}
	return models
}

func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	promptContent, _ := config.LoadPrompt(cfg.PromptFile)
	render(w, "settings.html", pageData{
		ActivePage: "settings",
		Data: map[string]any{
			"Config": cfg,
			"Prompt": promptContent,
			"Models": fetchModels(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.EmbedModel),
		},
	})
}

func handleLogsPage(w http.ResponseWriter, r *http.Request) {
	render(w, "logs.html", pageData{
		ActivePage: "logs",
		Data:       logger.All(),
	})
}

// ── HTMX partial handlers ────────────────────────────────────────────────────

func handleAnalyzeForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	idStr := r.FormValue("documentId")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		renderPartial(w, "analyze_result.html", map[string]any{"Error": "Please enter a valid document ID"})
		return
	}
	resp, err := analyzeDocument(id, nil)
	if err != nil {
		renderPartial(w, "analyze_result.html", map[string]any{"Error": err.Error()})
		return
	}
	renderPartial(w, "analyze_result.html", map[string]any{
		"DocumentID":     id,
		"Analysis":       resp.Analysis,
		"Perf":           resp.Perf,
		"Document":       resp.Document,
		"Correspondents": resp.Correspondents,
		"DocumentTypes":  resp.DocumentTypes,
		"AllTags":        resp.Tags,
	})
}

func handleSaveDocument(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.Atoi(idStr)
	r.ParseForm()

	title := r.FormValue("title")
	createdDate := r.FormValue("created_date")
	correspondent := r.FormValue("correspondent")
	documentType := r.FormValue("document_type")
	tagsRaw := r.FormValue("tags")

	var tags []string
	for _, t := range strings.Split(tagsRaw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}

	var corrPtr *string
	if correspondent != "" {
		corrPtr = &correspondent
	}
	var dtPtr *string
	if documentType != "" {
		dtPtr = &documentType
	}
	var datePtr *string
	if createdDate != "" {
		datePtr = &createdDate
	}

	result := types.AnalysisResult{
		Title:         title,
		Tags:          tags,
		CreatedDate:   datePtr,
		Correspondent: corrPtr,
		DocumentType:  dtPtr,
	}

	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	doc, err := pc.GetDocument(id)
	if err != nil {
		renderPartial(w, "analyze_result.html", map[string]any{"Error": err.Error()})
		return
	}
	existingTags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()

	var tagIDs []int
	for _, name := range result.Tags {
		tid, err := pc.ResolveOrCreate("tag", name, existingTags)
		if err == nil && tid != nil {
			tagIDs = append(tagIDs, *tid)
		}
	}
	var corrID *int
	if result.Correspondent != nil {
		corrID, _ = pc.ResolveOrCreate("correspondent", *result.Correspondent, correspondents)
	}
	var dtID *int
	if result.DocumentType != nil {
		dtID, _ = pc.ResolveOrCreate("document_type", *result.DocumentType, docTypes)
	}
	created := doc.Created
	if result.CreatedDate != nil {
		created = *result.CreatedDate + "T00:00:00Z"
	}
	update := types.DocumentUpdate{
		Title:         result.Title,
		Created:       created,
		Correspondent: corrID,
		DocumentType:  dtID,
		Tags:          tagIDs,
	}
	if err := pc.UpdateDocument(id, update); err != nil {
		renderPartial(w, "analyze_result.html", map[string]any{"Error": err.Error()})
		return
	}
	logger.Info(fmt.Sprintf("Saved doc %d: %s", id, result.Title))
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<p class="flash">Document %d saved successfully: %s</p>`, id, template.HTMLEscapeString(result.Title))
}

func handleScanRunForm(w http.ResponseWriter, r *http.Request) {
	if scanner.IsRunning() {
		renderPartial(w, "scan_status.html", map[string]any{
			"Status": scanner.GetStatus(),
			"Flash":  "Scan is already running",
		})
		return
	}
	go scanner.RunOnce()
	time.Sleep(200 * time.Millisecond) // Brief wait so status reflects running=true
	renderPartial(w, "scan_status.html", map[string]any{
		"Status": scanner.GetStatus(),
		"Flash":  "Scan started",
	})
}

func handleScanStatusPartial(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "scan_status.html", map[string]any{
		"Status": scanner.GetStatus(),
	})
}

func handleLogsPartial(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "logs_partial.html", logger.All())
}

func handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	interval, _ := strconv.Atoi(r.FormValue("scanInterval"))
	strict := r.FormValue("strictMetadata") == "on"
	think := r.FormValue("think") == "on"
	temperature, _ := strconv.ParseFloat(r.FormValue("temperature"), 64)
	topK, _ := strconv.Atoi(r.FormValue("topK"))
	topP, _ := strconv.ParseFloat(r.FormValue("topP"), 64)
	repeatPenalty, _ := strconv.ParseFloat(r.FormValue("repeatPenalty"), 64)
	numCtx, _ := strconv.Atoi(r.FormValue("numCtx"))
	cfg := &types.AppConfig{
		OpenAIBaseURL:  r.FormValue("openaiBaseUrl"),
		OpenAIModel:    r.FormValue("openaiModel"),
		PromptFile:     r.FormValue("promptFile"),
		PaperlessURL:   r.FormValue("paperlessBaseUrl"),
		PaperlessToken: r.FormValue("paperlessApiToken"),
		Port:           r.FormValue("port"),
		ScanTag:        r.FormValue("scanTag"),
		ScanInterval:   interval,
		AITag:          r.FormValue("aiTag"),
		StrictMetadata: strict,
		EmbedModel:     r.FormValue("embedModel"),
		IndexPath:      r.FormValue("indexPath"),
		Think:          think,
		Temperature:    temperature,
		TopK:           topK,
		TopP:           topP,
		RepeatPenalty:  repeatPenalty,
		NumCtx:         numCtx,
		MCPToken:       r.FormValue("mcpToken"),
		OpenAIAPIKey:   r.FormValue("openaiApiKey"),
	}
	prompt, _ := config.LoadPrompt(cfg.PromptFile)
	models := fetchModels(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.EmbedModel)
	if err := config.Save(cfg); err != nil {
		renderPartial(w, "settings_form.html", map[string]any{"Config": cfg, "Models": models, "Prompt": prompt, "Error": err.Error()})
		return
	}
	scanner.Stop()
	if cfg.ScanInterval > 0 {
		scanner.Start(cfg.ScanInterval)
	}
	renderPartial(w, "settings_form.html", map[string]any{"Config": cfg, "Models": models, "Prompt": prompt, "Flash": "Settings saved"})
}

func handlePromptForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	content := r.FormValue("prompt")
	cfg, _ := config.Load()
	if err := config.SavePrompt(cfg.PromptFile, content); err != nil {
		renderPartial(w, "prompt_form.html", map[string]any{"Prompt": content, "Error": err.Error()})
		return
	}
	renderPartial(w, "prompt_form.html", map[string]any{"Prompt": content, "Flash": "Prompt saved"})
}

func handleChatPage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)
	models, _ := oc.ListModels()
	render(w, "chat.html", pageData{
		ActivePage: "chat",
		Data: map[string]any{
			"IndexStatus":  enrichedIndexStatus(),
			"Models":       models,
			"DefaultModel": cfg.OpenAIModel,
			"ThinkDefault": cfg.Think,
		},
	})
}

func handleChatForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	question := strings.TrimSpace(r.FormValue("question"))
	chatModel := r.FormValue("chatModel")
	if question == "" {
		renderPartial(w, "chat_answer.html", map[string]any{"Error": "Please enter a question"})
		return
	}

	// Parse conversation history from hidden field
	var history []types.ConversationMessage
	if raw := r.FormValue("history"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &history)
	}

	// Parse pinned document IDs from previous turns
	var pinnedDocIDs []int
	if raw := r.FormValue("pinned_ids"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &pinnedDocIDs)
	}

	// Think toggle: use form value if present, otherwise fall back to config default
	var think *bool
	if v := r.FormValue("think"); v != "" {
		b := v == "true"
		think = &b
	}

	jobID := newJobID()
	job := &chatJob{
		status:   "Starting…",
		question: question,
		model:    chatModel,
	}
	chatJobs.Store(jobID, job)

	cfg, _ := config.Load()
	go func() {
		oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		logger.Info(fmt.Sprintf("RAG query [%s]: %s", chatModel, question))

		onStatus := func(s string) {
			job.mu.Lock()
			job.status = s
			job.mu.Unlock()
		}

		result, err := rag.Global().Query(question, cfg.PaperlessURL, oc, cfg.EmbedModel, chatModel, history, pc, pinnedDocIDs, think, onStatus)

		job.mu.Lock()
		defer job.mu.Unlock()
		job.done = true
		if err != nil {
			logger.Error("RAG query failed", err.Error())
			job.data = map[string]any{"Error": err.Error()}
		} else {
			logger.Info(fmt.Sprintf("RAG query answered with %d sources", len(result.Sources)))
			historyJSON, _ := json.Marshal(result.History)
			// Round-robin pinned doc buffer (max 10): new sources take priority,
			// old pinned IDs fill the remainder.
			const maxPinnedDocs = 10
			oldSet := make(map[int]bool, len(pinnedDocIDs))
			for _, id := range pinnedDocIDs {
				oldSet[id] = true
			}
			var newIDs []int
			for _, s := range result.Sources {
				if !oldSet[s.ID] {
					newIDs = append(newIDs, s.ID)
				}
			}
			seen := make(map[int]bool, maxPinnedDocs)
			allPinnedIDs := make([]int, 0, maxPinnedDocs)
			for _, id := range newIDs {
				if !seen[id] && len(allPinnedIDs) < maxPinnedDocs {
					allPinnedIDs = append(allPinnedIDs, id)
					seen[id] = true
				}
			}
			for _, id := range pinnedDocIDs {
				if len(allPinnedIDs) >= maxPinnedDocs {
					break
				}
				if !seen[id] {
					allPinnedIDs = append(allPinnedIDs, id)
					seen[id] = true
				}
			}
			pinnedIDsJSON, _ := json.Marshal(allPinnedIDs)
			job.data = map[string]any{
				"Question":      question,
				"Model":         chatModel,
				"Result":        result,
				"HistoryJSON":   string(historyJSON),
				"PinnedIDsJSON": string(pinnedIDsJSON),
			}
		}
	}()

	renderPartial(w, "chat_loading.html", map[string]any{
		"JobID":    jobID,
		"Question": question,
		"Status":   "Starting…",
	})
}

func handleChatStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	val, ok := chatJobs.Load(jobID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	job := val.(*chatJob)

	job.mu.Lock()
	defer job.mu.Unlock()

	if !job.done {
		renderPartial(w, "chat_loading.html", map[string]any{
			"JobID":    jobID,
			"Question": job.question,
			"Status":   job.status,
		})
		return
	}

	chatJobs.Delete(jobID)
	renderPartial(w, "chat_answer.html", job.data)
}

func handleIndexRun(w http.ResponseWriter, r *http.Request) {
	if rag.Global().IsRunning() {
		renderPartial(w, "index_status.html", map[string]any{
			"Status": enrichedIndexStatus(),
			"Flash":  "Indexing already running",
		})
		return
	}
	r.ParseForm()
	full := r.FormValue("full") == "true"
	go rag.Global().RunOnce(full)
	flash := "Sync started"
	if full {
		flash = "Full re-index started"
	}
	renderPartial(w, "index_status.html", map[string]any{
		"Status": enrichedIndexStatus(),
		"Flash":  flash,
	})
}

func handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "index_status.html", map[string]any{
		"Status": enrichedIndexStatus(),
	})
}

func enrichedIndexStatus() types.IndexStatus {
	s := rag.Global().GetStatus()
	cfg, err := config.Load()
	if err != nil {
		return s
	}
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	if total, err := pc.CountDocuments(); err == nil {
		s.PaperlessTotal = total
		s.MissingCount = total - s.DocCount
		if s.MissingCount < 0 {
			s.MissingCount = 0
		}
	}
	return s
}

func handleTestPage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)
	models, _ := oc.ListModels()
	render(w, "test.html", pageData{
		ActivePage: "test",
		Data: map[string]any{
			"Config": cfg,
			"Models": models,
		},
	})
}

func resolveName(id *int, items []types.PaperlessItem) string {
	if id == nil {
		return ""
	}
	for _, item := range items {
		if item.ID == *id {
			return item.Name
		}
	}
	return ""
}

func resolveTagNames(ids []int, items []types.PaperlessItem) []string {
	byID := make(map[int]string, len(items))
	for _, item := range items {
		byID[item.ID] = item.Name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := byID[id]; ok {
			names = append(names, name)
		}
	}
	return names
}

func handleTestCompare(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	docID, err := strconv.Atoi(r.FormValue("documentId"))
	if err != nil || docID <= 0 {
		renderPartial(w, "test_result.html", map[string]any{"Error": "Please enter a valid document ID"})
		return
	}

	temperature, _ := strconv.ParseFloat(r.FormValue("b_temperature"), 64)
	topK, _ := strconv.Atoi(r.FormValue("b_topK"))
	topP, _ := strconv.ParseFloat(r.FormValue("b_topP"), 64)
	repeatPenalty, _ := strconv.ParseFloat(r.FormValue("b_repeatPenalty"), 64)
	numCtx, _ := strconv.Atoi(r.FormValue("b_numCtx"))
	optsB := types.ModelOptions{
		Model:         r.FormValue("b_model"),
		Temperature:   temperature,
		TopK:          topK,
		TopP:          topP,
		RepeatPenalty: repeatPenalty,
		NumCtx:        numCtx,
		Think:         r.FormValue("b_think") == "on",
		UnloadAfter:   true,
	}

	// Load current config opts for Side A display
	cfg, _ := config.Load()
	optsA := types.ModelOptions{
		Model:         cfg.OpenAIModel,
		Temperature:   cfg.Temperature,
		TopK:          cfg.TopK,
		TopP:          cfg.TopP,
		RepeatPenalty: cfg.RepeatPenalty,
		NumCtx:        cfg.NumCtx,
		Think:         cfg.Think,
	}

	// Run sequentially so models don't compete for GPU memory
	sideA := map[string]any{"Opts": optsA}
	respA, errA := analyzeDocument(docID, nil)
	if errA != nil {
		sideA["Error"] = errA.Error()
	} else {
		sideA["Resp"] = respA
	}

	sideB := map[string]any{"Opts": optsB}
	respB, errB := analyzeDocument(docID, &optsB)
	if errB != nil {
		sideB["Error"] = errB.Error()
	} else {
		sideB["Resp"] = respB
	}

	// Build current paperless values from first successful response
	var current map[string]any
	var correspondents, docTypes, allTags []types.PaperlessItem
	ref := respA
	if ref == nil {
		ref = respB
	}
	if ref != nil {
		correspondents = ref.Correspondents
		docTypes = ref.DocumentTypes
		allTags = ref.Tags
		doc := ref.Document
		current = map[string]any{
			"Title":        doc.Title,
			"Created":      doc.Created,
			"Correspondent": resolveName(doc.Correspondent, correspondents),
			"DocumentType": resolveName(doc.DocumentType, docTypes),
			"Tags":         resolveTagNames(doc.Tags, allTags),
		}
	}

	renderPartial(w, "test_result.html", map[string]any{
		"DocID":          docID,
		"A":              sideA,
		"B":              sideB,
		"Current":        current,
		"Correspondents": correspondents,
		"DocumentTypes":  docTypes,
		"AllTags":        allTags,
	})
}

func handleApplyVariant(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	temperature, _ := strconv.ParseFloat(r.FormValue("temperature"), 64)
	topK, _ := strconv.Atoi(r.FormValue("topK"))
	topP, _ := strconv.ParseFloat(r.FormValue("topP"), 64)
	repeatPenalty, _ := strconv.ParseFloat(r.FormValue("repeatPenalty"), 64)
	numCtx, _ := strconv.Atoi(r.FormValue("numCtx"))
	think := r.FormValue("think") == "true"
	model := r.FormValue("model")

	cfg, err := config.Load()
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<p class="error-msg">Failed to load config: %s</p>`, template.HTMLEscapeString(err.Error()))
		return
	}
	if model != "" {
		cfg.OpenAIModel = model
	}
	cfg.Temperature = temperature
	cfg.TopK = topK
	cfg.TopP = topP
	cfg.RepeatPenalty = repeatPenalty
	cfg.NumCtx = numCtx
	cfg.Think = think

	if err := config.Save(cfg); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<p class="error-msg">Failed to save: %s</p>`, template.HTMLEscapeString(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<p class="flash">Variant applied — Settings updated.</p>`))
}

func namesOf(items []types.PaperlessItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}

// ── Auth handlers ────────────────────────────────────────────────────────────

func handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if authManager.IsSetup() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	renderAuth(w, "setup.html", map[string]any{})
}

func handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if authManager.IsSetup() {
		http.Redirect(w, r, "/chat", http.StatusFound)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	if password != password2 {
		renderAuth(w, "setup.html", map[string]any{"Error": "Passwords do not match.", "Username": username})
		return
	}
	if err := authManager.Setup(username, password); err != nil {
		renderAuth(w, "setup.html", map[string]any{"Error": err.Error(), "Username": username})
		return
	}
	token, _ := authManager.Login(username, password)
	authManager.SetCookie(w, token)
	http.Redirect(w, r, "/chat", http.StatusFound)
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !authManager.IsSetup() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	renderAuth(w, "login.html", map[string]any{})
}

func handleLoginForm(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	token, err := authManager.Login(username, password)
	if err != nil {
		renderAuth(w, "login.html", map[string]any{"Error": "Invalid username or password.", "Username": username})
		return
	}
	authManager.SetCookie(w, token)
	http.Redirect(w, r, "/chat", http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	token := authManager.TokenFromRequest(r)
	authManager.Logout(token)
	authManager.ClearCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// renderAuth renders standalone auth pages (no layout, no nav).
func renderAuth(w http.ResponseWriter, page string, data map[string]any) {
	t, err := template.ParseFS(assets.FS, "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
