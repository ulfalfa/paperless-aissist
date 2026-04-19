package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/promptlog"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
)

// Query uses Ollama /api/chat with tool calling. The LLM can call
// search_documents_semantic to perform a vector search, get_document for full
// content, and other paperless tools. Previously found documents (pinnedDocIDs)
// are loaded upfront and provided as context so the LLM skips re-searching on
// follow-up questions.
func (idx *Indexer) Query(
	question, paperlessBaseURL string,
	oc *ollama.Client,
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
		"- If the user provides an exact document title or a specific name to look up, call search_documents with that title/name.\n" +
		"- For all other questions about topics, subjects, or content, call search_documents_semantic with a descriptive query.\n" +
		"Call search_documents_semantic at most once per user question. " +
		"Only call list_tags, list_correspondents, list_document_types, count_documents when the user explicitly asks about those metadata categories. " +
		"Call search_documents_semantic again only if the user asks about a topic not covered by the pinned documents above."

	messages := []ollama.ChatMessage{
		{Role: "system", Content: systemContent},
	}
	for _, h := range history {
		messages = append(messages, ollama.ChatMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, ollama.ChatMessage{Role: "user", Content: question})

	// ── 3. Tool-call loop ─────────────────────────────────────────────────────
	ollamaTools := buildTools()
	const maxRounds = 4
	var answer string
	start := time.Now()
	perf := &types.ChatPerf{}
	ragMeta := map[string]string{"pinned_count": strconv.Itoa(len(pinnedDocIDs))}

	for round := 0; round < maxRounds; round++ {
		status("Thinking…")

		llmStart := time.Now()
		msg, rp, err := oc.Chat(chatModel, messages, ollamaTools, think)
		llmDuration := float64(time.Since(llmStart).Milliseconds())
		if err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		perf.LLMRounds++
		if rp != nil {
			perf.GeneratedTokens += rp.GeneratedTokens
			perf.TokensPerSec = rp.TokensPerSec
		}

		var meta map[string]string
		if round == 0 {
			meta = ragMeta
		}
		promptlog.WriteLLM(round, chatModel, messages, ollamaTools, msg, llmDuration, rp.GeneratedTokens, rp.TokensPerSec, meta)

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

			messages = append(messages, ollama.ChatMessage{
				Role:    "tool",
				Content: text,
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
func buildTools() []ollama.ChatTool {
	noParams := json.RawMessage(`{"type":"object","properties":{}}`)
	return []ollama.ChatTool{
		{
			Type: "function",
			Function: ollama.ChatToolFunction{
				Name: "search_documents_semantic",
				Description: "Search the document archive using semantic/vector search. " +
					"Use this to find documents relevant to a topic or question. " +
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
			Function: ollama.ChatToolFunction{
				Name:        "search_documents",
				Description: "Search paperless-ngx by keyword, exact title, or name. Use this when the user provides an exact document title, a specific person or company name, or other exact terms. Prefer this over semantic search for exact lookups.",
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
			Function: ollama.ChatToolFunction{
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
			Function: ollama.ChatToolFunction{
				Name:        "list_tags",
				Description: "List all tags in the paperless-ngx archive with their document counts.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: ollama.ChatToolFunction{
				Name:        "list_correspondents",
				Description: "List all correspondents (senders/recipients) in the paperless-ngx archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: ollama.ChatToolFunction{
				Name:        "list_document_types",
				Description: "List all document types in the paperless-ngx archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: ollama.ChatToolFunction{
				Name:        "count_documents",
				Description: "Return the total number of documents stored in the archive.",
				Parameters:  noParams,
			},
		},
		{
			Type: "function",
			Function: ollama.ChatToolFunction{
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
			Function: ollama.ChatToolFunction{
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
