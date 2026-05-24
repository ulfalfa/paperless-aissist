package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/openai"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/promptlog"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
)

// Query uses /v1/chat/completions with tool calling. The LLM can call
// search_documents_semantic to perform a vector search, get_document for full
// content, and other paperless tools. Previously found documents (pinnedDocIDs)
// are loaded upfront and provided as context so the LLM skips re-searching on
// follow-up questions.
func (idx *Indexer) Query(
	question, paperlessBaseURL string,
	oc *openai.Client,
	embedModel, chatModel string,
	history []types.ConversationMessage,
	pc *paperless.Client,
	pinnedDocIDs []int,
	think *bool,
	onStatus func(string),
) (*types.ChatResult, error) {

	status := func(s string) {
		if onStatus != nil {
			onStatus(s)
		}
	}

	// ── 1. Load pinned documents from previous turns ──────────────────────────
	var pinnedContext string
	var sources []types.ChatSource

	if len(pinnedDocIDs) > 0 {
		status("Loading pinned documents...")
		var parts []string
		for _, id := range pinnedDocIDs {
			doc, err := pc.GetDocument(id)
			if err != nil {
				logger.Warn(fmt.Sprintf("RAG: failed to load pinned doc %d", id), err.Error())
				continue
			}
			parts = append(parts, fmt.Sprintf("[ID:%d] %s\n%s", doc.ID, doc.Title, doc.Content))
			sources = append(sources, types.ChatSource{
				ID:           doc.ID,
				Title:        doc.Title,
				PaperlessURL: strings.TrimRight(paperlessBaseURL, "/") + fmt.Sprintf("/documents/%d/details", doc.ID),
			})
		}
		pinnedContext = strings.Join(parts, "\n\n")
	}

	// ── 2. Build system prompt ────────────────────────────────────────────────
	systemContent := "You are an assistant for a personal document archive.\n" +
		"Be concise and specific. Always respond in the same language the user used.\n" +
		"When your answer is based on specific documents, always mention their exact title in your response " +
		"so the user can identify them. Use the title exactly as it appears in the document metadata.\n\n"

	if pinnedContext != "" {
		systemContent += "The following documents were already found in this conversation. " +
			"Use them to answer follow-up questions WITHOUT calling search_documents_semantic again " +
			"unless the user is clearly asking about a different topic.\n\n" +
			pinnedContext + "\n\n"
	}

	systemContent += "To find documents, follow these rules:\n" +
		"- If the user provides a numeric document ID, call get_document with that ID directly.\n" +
		"- For questions about topics, events, or transactions: call search_documents_semantic with an enriched query. " +
		"Infer the implied document type (e.g. Überweisung→Kontoauszug, Steuer→Steuerbescheid, " +
		"Versicherung→Police, Gehalt→Lohnabrechnung) and include it with any person/company names in the query.\n" +
		"- If the question names a specific person, company, or organization: also call search_documents with that name " +
		"in the same round (parallel with search_documents_semantic).\n" +
		"- For exact document title lookups only: call search_documents with the title alone.\n" +
		"Only call list_tags, list_correspondents, list_document_types, count_documents when the user explicitly asks about those metadata categories. " +
		"Do not call search_documents_semantic again if pinned documents above already answer the question."

	messages := []openai.ChatMessage{
		{Role: "system", Content: systemContent},
	}
	for _, h := range history {
		messages = append(messages, openai.ChatMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, openai.ChatMessage{Role: "user", Content: question})

	// ── 3. Tool-call loop ─────────────────────────────────────────────────────
	chatTools := buildTools()
	const maxRounds = 4
	var answer string
	start := time.Now()
	perf := &types.ChatPerf{ContextSize: oc.ContextSize(chatModel)}
	ragMeta := map[string]string{"pinned_count": strconv.Itoa(len(pinnedDocIDs))}

	for round := 0; round < maxRounds; round++ {
		status("Thinking…")

		llmStart := time.Now()
		msg, rp, err := oc.Chat(chatModel, messages, chatTools, think)
		llmDuration := float64(time.Since(llmStart).Milliseconds())
		if err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		perf.LLMRounds++
		if rp != nil {
			perf.GeneratedTokens += rp.GeneratedTokens
			perf.PromptTokens = rp.PromptTokens // last round is most representative (longest context)
			perf.TokensPerSec = rp.TokensPerSec
		}

		var meta map[string]string
		if round == 0 {
			meta = ragMeta
		}
		promptlog.WriteLLM(round, chatModel, messages, chatTools, msg, llmDuration, rp.GeneratedTokens, rp.TokensPerSec, meta)

		messages = append(messages, *msg)

		if len(msg.ToolCalls) == 0 {
			answer = msg.Content
			break
		}

		for _, tc := range msg.ToolCalls {
			status(fmt.Sprintf("Calling tool: %s…", tc.Function.Name))
			toolStart := time.Now()

			var text, errStr string

			if tc.Function.Name == "search_documents_semantic" {
				// Handle inline: perform vector search
				var args struct {
					Query   string `json:"query"`
					MaxDocs int    `json:"max_docs"`
				}
				_ = json.Unmarshal(tc.Function.Arguments, &args)
				if args.MaxDocs <= 0 {
					args.MaxDocs = 5
				}

				emb, embedErr := oc.Embed(embedModel, args.Query)
				if embedErr != nil {
					text = fmt.Sprintf("error embedding query: %v", embedErr)
					errStr = embedErr.Error()
				} else {
					results, searchErr := idx.store.Search(context.Background(), emb, args.MaxDocs)
					if searchErr != nil {
						text = fmt.Sprintf("error searching index: %v", searchErr)
						errStr = searchErr.Error()
					} else if len(results) == 0 {
						text = "No documents found for this query."
					} else {
						var parts []string
						for _, r := range results {
							title := r.Metadata["title"]
							docID, _ := strconv.Atoi(r.ID)
							parts = append(parts, fmt.Sprintf("[ID:%d] %s\n%s", docID, title, r.Content))
							sources = appendUniqueSources(sources, []types.ChatSource{{
								ID:           docID,
								Title:        title,
								ScorePercent: int(r.Similarity * 100),
								PaperlessURL: strings.TrimRight(paperlessBaseURL, "/") + fmt.Sprintf("/documents/%d/details", docID),
							}})
						}
						text = strings.Join(parts, "\n\n")
					}
				}
				ragMeta["rag_query"] = args.Query
				ragMeta["rag_count"] = strconv.Itoa(len(sources))
			} else {
				tr, err := tools.Execute(tc.Function.Name, tc.Function.Arguments, pc, paperlessBaseURL)
				if err != nil {
					text = fmt.Sprintf("error: %v", err)
					errStr = err.Error()
				} else {
					text = tr.Text
					sources = appendUniqueSources(sources, tr.Sources)
				}
			}

			toolDuration := float64(time.Since(toolStart).Milliseconds())
			perf.ToolCalls++
			promptlog.WriteTool(round, tc.Function.Name, tc.Function.Arguments, text, errStr, toolDuration)

			messages = append(messages, openai.ChatMessage{
				Role:       "tool",
				Content:    text,
				ToolCallID: tc.ID,
			})
		}
	}
	perf.TotalMs = float64(time.Since(start).Milliseconds())

	if answer == "" {
		answer = "Could not generate an answer."
	}

	updatedHistory := append(history,
		types.ConversationMessage{Role: "user", Content: question},
		types.ConversationMessage{Role: "assistant", Content: answer},
	)

	return &types.ChatResult{
		Answer:  answer,
		Sources: sources,
		History: updatedHistory,
		Perf:    perf,
	}, nil
}

// buildTools returns the tool definitions for all supported paperless operations.
func buildTools() []openai.ChatTool {
	noParams := json.RawMessage(`{"type":"object","properties":{}}`)
	return []openai.ChatTool{
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name: "search_documents_semantic",
				Description: "Search the document archive by content similarity (vector search). " +
					"Before writing the query, infer what type of document would contain the answer — for example: " +
					"a question about a bank transfer implies 'Kontoauszug', taxes imply 'Steuerbescheid', " +
					"insurance implies 'Police' or 'Rechnung', employment implies 'Arbeitsvertrag' or 'Zeugnis'. " +
					"Include the inferred document type AND any person/company names in the query string. " +
					"Example: for 'Wann habe ich an Müller überwiesen?' use query 'Überweisung Müller Kontoauszug'. " +
					"IMPORTANT: Results are sorted by content similarity, NOT by date. " +
					"For questions like 'latest/newest/most recent X' use find_documents with correspondent_name or document_type_name instead — it guarantees chronological order. " +
					"Do NOT call this if pinned documents in the system prompt already cover the topic.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Descriptive search query"},
						"max_docs": {"type": "integer", "description": "Max results to return (default 5)"}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "search_documents",
				Description: "Search paperless-ngx by keyword, exact title, or name. " +
					"Use this when the user mentions a specific person, company, or organization name, or an exact document title. " +
					"You may call this in the same round as search_documents_semantic — both tools can run together.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Search query"},
						"page_size": {"type": "integer", "description": "Max results (default 10)"}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "get_document",
				Description: "Retrieve full details and content of a specific document by its numeric ID. Use this immediately when the user mentions or provides a document ID number.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"id": {"type": "integer", "description": "Document ID"}
					},
					"required": ["id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_tags",
				Description: "List all tags in the paperless-ngx archive with their document counts.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_correspondents",
				Description: "List all correspondents (senders/recipients) in the paperless-ngx archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_document_types",
				Description: "List all document types in the paperless-ngx archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "count_documents",
				Description: "Return the total number of documents stored in the archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_documents_by_type",
				Description: "List all documents with a given document type (e.g. 'Zeugnis', 'Rechnung').",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"type_name": {"type": "string", "description": "Document type name (case-insensitive)"}
					},
					"required": ["type_name"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_documents_by_correspondent",
				Description: "List all documents from a specific correspondent (sender/recipient).",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"correspondent_name": {"type": "string", "description": "Correspondent name (case-insensitive)"}
					},
					"required": ["correspondent_name"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name: "find_documents",
				Description: "Find documents using combined filters: text query, correspondent, document type, tags, and date range. " +
					"Best choice for RECENCY questions ('die letzte Rechnung von Telekom', 'letzter Werkstattbesuch Galaxy', 'aktuellste Unterlagen von X'): " +
					"set correspondent_name or document_type_name, leave ordering at default (-created) — returns the absolute newest document first, guaranteed. " +
					"Filters via Paperless metadata, not full-text — always complete, never misses a new document. " +
					"Examples: 'Rechnung von Telekom aus 2024' → correspondent_name=Telekom, document_type_name=Rechnung, created_from=2024-01-01, created_to=2024-12-31. " +
					"'Was kam im März von der Krankenkasse?' → correspondent_name=Krankenkasse, created_from=YYYY-03-01, created_to=YYYY-03-31. " +
					"Use limit to cap the result size.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query":             {"type": "string",  "description": "Optional freetext search query"},
						"correspondent_name":{"type": "string",  "description": "Filter by correspondent name (case-insensitive)"},
						"document_type_name":{"type": "string",  "description": "Filter by document type name (case-insensitive)"},
						"tag_names":         {"type": "array", "items": {"type": "string"}, "description": "Filter by one or more tag names"},
						"created_from":      {"type": "string",  "description": "Earliest creation date inclusive (YYYY-MM-DD)"},
						"created_to":        {"type": "string",  "description": "Latest creation date inclusive (YYYY-MM-DD)"},
						"ordering":          {"type": "string",  "description": "Sort order, e.g. '-created' (default) or 'created'"},
						"limit":             {"type": "integer", "description": "Max results to return (default 50)"}
					},
					"properties": {}
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "list_documents_by_tag",
				Description: "List all documents with a specific tag. Use when the user asks for documents with a given label or category.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"tag_name": {"type": "string", "description": "Tag name (case-insensitive)"}
					},
					"required": ["tag_name"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name: "list_recent_documents",
				Description: "List the most recently created documents across the entire archive, without a topic filter. " +
					"Use for 'what came in recently', 'what's new this week', 'what was added this month'. " +
					"NOT for 'the latest invoice from X' or 'last visit to Y' — use find_documents with correspondent_name for those.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"limit": {"type": "integer", "description": "Max results (default 10)"},
						"since": {"type": "string",  "description": "Only include documents created on or after this date (YYYY-MM-DD)"}
					},
					"properties": {}
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "add_tag_to_document",
				Description: "Add a tag to a document. Use when the user wants to label or categorize a document. Set create_if_missing=true to create the tag if it doesn't exist yet.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"id":               {"type": "integer", "description": "Document ID"},
						"tag_name":         {"type": "string",  "description": "Tag name to add"},
						"create_if_missing":{"type": "boolean", "description": "Create the tag if it does not exist (default false)"}
					},
					"required": ["id", "tag_name"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "remove_tag_from_document",
				Description: "Remove a tag from a document. Idempotent — no error if the tag is not on the document.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"id":      {"type": "integer", "description": "Document ID"},
						"tag_name":{"type": "string",  "description": "Tag name to remove"}
					},
					"required": ["id", "tag_name"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name:        "download_document",
				Description: "Download the original file of a document as a base64-encoded string. Use when the user wants to inspect the actual PDF content or process the file directly.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"id": {"type": "integer", "description": "Document ID"}
					},
					"required": ["id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openai.ChatToolFunction{
				Name: "upload_document",
				Description: "Upload a new document to the paperless-ngx archive. " +
					"The file must be provided as a base64-encoded string. " +
					"Optionally set title, correspondent, document type, tags, and creation date. " +
					"By default waits up to 60 seconds for paperless to finish processing.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"filename":          {"type": "string",  "description": "Original filename including extension (e.g. 'invoice.pdf')"},
						"base64_content":    {"type": "string",  "description": "Base64-encoded file content"},
						"title":             {"type": "string",  "description": "Document title (optional)"},
						"correspondent_name":{"type": "string",  "description": "Correspondent name — created if missing (optional)"},
						"document_type_name":{"type": "string",  "description": "Document type name — created if missing (optional)"},
						"tag_names":         {"type": "array", "items": {"type": "string"}, "description": "Tags to assign — created if missing (optional)"},
						"created":           {"type": "string",  "description": "Document creation date (YYYY-MM-DD, optional)"},
						"wait":              {"type": "boolean", "description": "Wait for processing to complete (default true)"}
					},
					"required": ["filename", "base64_content"]
				}`),
			},
		},
	}
}

// appendUniqueSources adds sources not already present (by ID) to the slice.
func appendUniqueSources(existing, extra []types.ChatSource) []types.ChatSource {
	seen := make(map[int]bool, len(existing))
	for _, s := range existing {
		seen[s.ID] = true
	}
	for _, s := range extra {
		if !seen[s.ID] {
			existing = append(existing, s)
			seen[s.ID] = true
		}
	}
	return existing
}
