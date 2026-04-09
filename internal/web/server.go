package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"sort"
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
	"github.com/ulf/paperless-aissist/internal/ollama"
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
		"derefInt": func(i *int) int {
			if i == nil {
				return 0
			}
			return *i
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
		"templates/analyze_result.html",
		"templates/tag_docs.html",
		"templates/tag_analysis.html",
		"templates/scan_status.html",
		"templates/settings_form.html",
		"templates/prompt_form.html",
		"templates/logs_partial.html",
		"templates/index_status.html",
		"templates/chat_answer.html",
		"templates/chat_loading.html",
	))
}

// pageTmpl loads layout + all partials + a specific page file into an isolated
// template set. Including only one page file avoids {{define "content"}} conflicts.
func pageTmpl(page string) *template.Template {
	return template.Must(template.New("").Funcs(tmplFunc).ParseFS(assets.FS,
		"templates/layout.html",
		"templates/analyze_result.html",
		"templates/tag_docs.html",
		"templates/tag_analysis.html",
		"templates/scan_status.html",
		"templates/settings_form.html",
		"templates/prompt_form.html",
		"templates/logs_partial.html",
		"templates/index_status.html",
		"templates/chat_answer.html",
		"templates/chat_loading.html",
		"templates/"+page,
	))
}

func NewRouter(am *auth.Manager, paperlessToken string) http.Handler {
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

	// MCP endpoint — protected by paperless API token, not session
	mcpHandler := mcp.NewHTTPHandler()
	tokenGuard := auth.TokenMiddleware(paperlessToken)
	r.With(tokenGuard).Handle("/mcp", mcpHandler)
	r.With(tokenGuard).Handle("/mcp/*", mcpHandler)

	// API
	r.Get("/api/config", handleGetConfig)
	r.Post("/api/config", handlePostConfig)
	r.Get("/api/prompt", handleGetPrompt)
	r.Post("/api/prompt", handlePostPrompt)
	r.Post("/api/analyze", handleAnalyze)
	r.Patch("/api/documents/{id}", handleUpdateDocument)
	r.Get("/api/metadata", handleGetMetadata)
	r.Get("/api/documents", handleListDocuments)
	r.Get("/api/scan/status", handleScanStatus)
	r.Post("/api/scan/run", handleScanRun)
	r.Get("/api/logs", handleGetLogs)
	r.Delete("/api/logs", handleClearLogs)

	// UI pages
	r.Get("/", handleAnalyzePage)
	r.Get("/scan", handleScanPage)
	r.Get("/cleanup", handleCleanupPage)
	r.Get("/chat", handleChatPage)
	r.Get("/settings", handleSettingsPage)
	r.Get("/logs", handleLogsPage)

	// HTMX partials
	r.Post("/ui/analyze", handleAnalyzeForm)
	r.Post("/ui/documents/{id}/save", handleSaveDocument)
	r.Post("/ui/scan/run", handleScanRunForm)
	r.Get("/ui/scan/status", handleScanStatusPartial)
	r.Get("/ui/logs", handleLogsPartial)
	r.Get("/ui/tags/docs", handleTagDocsPartial)
	r.Post("/ui/cleanup/load", handleLoadTags)
	r.Post("/ui/cleanup/analyze", handleAnalyzeTags)
	r.Post("/ui/cleanup/merge", handleMergeTags)
	r.Post("/ui/cleanup/delete", handleDeleteTag)
	r.Post("/ui/settings", handleSettingsForm)
	r.Post("/ui/prompt", handlePromptForm)
	r.Post("/ui/chat", handleChatForm)
	r.Get("/ui/chat/status/{jobID}", handleChatStatus)
	r.Post("/ui/rag/index", handleIndexRun)
	r.Get("/ui/rag/status", handleIndexStatus)

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
	Perf           *types.OllamaPerf        `json:"perf"`
	Tags           []types.PaperlessItem    `json:"tags"`
	Correspondents []types.PaperlessItem    `json:"correspondents"`
	DocumentTypes  []types.PaperlessItem    `json:"documentTypes"`
}

func analyzeDocument(id int) (*analyzeResponse, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)

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
	prompt := ollama.BuildPrompt(promptTemplate, doc.Content, hints)
	result, perf, err := oc.Analyze(prompt)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
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
	resp, err := analyzeDocument(body.DocumentID)
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

func handleCleanupPage(w http.ResponseWriter, r *http.Request) {
	render(w, "cleanup.html", pageData{
		ActivePage: "cleanup",
		Data:       nil,
	})
}

func handleLoadTags(w http.ResponseWriter, r *http.Request) {
	logger.Info("Loading tags with document counts")
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	tags, err := pc.ListTagsWithCount()
	if err != nil {
		logger.Error("Failed to load tags", err.Error())
		renderPartial(w, "tag_list.html", map[string]any{"Error": err.Error()})
		return
	}
	// Sort by count descending
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Count > tags[j].Count
	})
	logger.Info(fmt.Sprintf("Loaded %d tags", len(tags)))
	renderPartial(w, "tag_list.html", tags)
}

func handleAnalyzeTags(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)

	logger.Info("Starting tag cleanup analysis")

	tags, err := pc.ListTagsWithCount()
	if err != nil {
		logger.Error("Failed to load tags for analysis", err.Error())
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Failed to load tags: " + err.Error()})
		return
	}
	if len(tags) == 0 {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "No tags found to analyze"})
		return
	}

	logger.Info(fmt.Sprintf("Sending %d tags to Ollama for analysis", len(tags)))
	analysis, err := oc.AnalyzeTags(tags)
	if err != nil {
		logger.Error("Tag cleanup analysis failed", err.Error())
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Analysis failed: " + err.Error()})
		return
	}

	logger.Info(fmt.Sprintf("Tag cleanup analysis complete: %d suggestions", len(analysis.Suggestions)))
	renderPartial(w, "tag_analysis.html", analysis)
}

func handleMergeTags(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	sourceID, _ := strconv.Atoi(r.FormValue("sourceId"))
	targetName := r.FormValue("targetName")

	if sourceID == 0 || targetName == "" {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Invalid parameters"})
		return
	}

	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

	// Find target tag ID
	tags, _ := pc.ListTags()
	var targetID *int
	for _, tag := range tags {
		if strings.EqualFold(tag.Name, targetName) {
			id := tag.ID
			targetID = &id
			break
		}
	}
	if targetID == nil {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Target tag not found: " + targetName})
		return
	}

	if err := pc.MergeTags(sourceID, *targetID); err != nil {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Merge failed: " + err.Error()})
		return
	}

	logger.Info(fmt.Sprintf("Merged tag %d into %d", sourceID, *targetID))
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<p class="flash">✓ Tags merged successfully. Re-analyze to see updated suggestions.</p>`)
}

func handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tagID, _ := strconv.Atoi(r.FormValue("tagId"))
	if tagID == 0 {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Invalid tag ID"})
		return
	}

	cfg, _ := config.Load()
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

	if err := pc.DeleteTag(tagID); err != nil {
		renderPartial(w, "tag_analysis.html", map[string]any{"Error": "Delete failed: " + err.Error()})
		return
	}

	logger.Info(fmt.Sprintf("Deleted tag %d", tagID))
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<p class="flash">✓ Tag deleted. Re-analyze to see updated suggestions.</p>`)
}

func handleScanPage(w http.ResponseWriter, r *http.Request) {
	render(w, "scan.html", pageData{
		ActivePage: "scan",
		Data:       scanner.GetStatus(),
	})
}

func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load()
	promptContent, _ := config.LoadPrompt(cfg.PromptFile)
	render(w, "settings.html", pageData{
		ActivePage: "settings",
		Data: map[string]any{
			"Config": cfg,
			"Prompt": promptContent,
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
	resp, err := analyzeDocument(id)
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
	cfg := &types.AppConfig{
		OllamaHost:     r.FormValue("ollamaHost"),
		OllamaModel:    r.FormValue("ollamaModel"),
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
	}
	if err := config.Save(cfg); err != nil {
		renderPartial(w, "settings_form.html", map[string]any{"Config": cfg, "Error": err.Error()})
		return
	}
	scanner.Stop()
	if cfg.ScanInterval > 0 {
		scanner.Start(cfg.ScanInterval)
	}
	renderPartial(w, "settings_form.html", map[string]any{"Config": cfg, "Flash": "Settings saved"})
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
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)
	models, _ := oc.ListModels()
	render(w, "chat.html", pageData{
		ActivePage: "chat",
		Data: map[string]any{
			"IndexStatus":  rag.Global().GetStatus(),
			"Models":       models,
			"DefaultModel": cfg.OllamaModel,
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

	jobID := newJobID()
	job := &chatJob{
		status:   "Starting…",
		question: question,
		model:    chatModel,
	}
	chatJobs.Store(jobID, job)

	cfg, _ := config.Load()
	go func() {
		oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		logger.Info(fmt.Sprintf("RAG query [%s]: %s", chatModel, question))

		onStatus := func(s string) {
			job.mu.Lock()
			job.status = s
			job.mu.Unlock()
		}

		result, err := rag.Global().Query(question, cfg.PaperlessURL, oc, cfg.EmbedModel, chatModel, history, pc, onStatus)

		job.mu.Lock()
		defer job.mu.Unlock()
		job.done = true
		if err != nil {
			logger.Error("RAG query failed", err.Error())
			job.data = map[string]any{"Error": err.Error()}
		} else {
			logger.Info(fmt.Sprintf("RAG query answered with %d sources", len(result.Sources)))
			historyJSON, _ := json.Marshal(result.History)
			job.data = map[string]any{
				"Question":    question,
				"Model":       chatModel,
				"Result":      result,
				"HistoryJSON": string(historyJSON),
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
			"Status": rag.Global().GetStatus(),
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
		"Status": rag.Global().GetStatus(),
		"Flash":  flash,
	})
}

func handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "index_status.html", map[string]any{
		"Status": rag.Global().GetStatus(),
	})
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
		http.Redirect(w, r, "/", http.StatusFound)
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
	http.Redirect(w, r, "/", http.StatusFound)
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
	http.Redirect(w, r, "/", http.StatusFound)
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
