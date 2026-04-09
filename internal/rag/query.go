package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
)

// Query embeds the question, finds the top-5 most relevant documents via vector
// search, then uses Ollama /api/chat with tool calling so the LLM can fetch live
// paperless-ngx data (tags, counts, search, etc.) for any question it cannot
// answer from the RAG context alone.
func (idx *Indexer) Query(
	question, paperlessBaseURL string,
	oc *ollama.Client,
	embedModel, chatModel string,
	history []types.ConversationMessage,
	pc *paperless.Client,
	onStatus func(string),
) (*types.ChatResult, error) {

	status := func(s string) {
		if onStatus != nil {
			onStatus(s)
		}
	}

	// ── 1. Vector search for document context ────────────────────────────────
	var ragContext string
	var sources []types.ChatSource

	status("Searching document index...")
	emb, err := oc.Embed(embedModel, question)
	if err == nil {
		ctx := context.Background()
		if results, err := idx.store.Search(ctx, emb, 5); err == nil && len(results) > 0 {
			var parts []string
			for i, r := range results {
				title := r.Metadata["title"]
				docID, _ := strconv.Atoi(r.ID)
				parts = append(parts, fmt.Sprintf("[%d] %s\n%s", i+1, title, r.Content))
				sources = append(sources, types.ChatSource{
					ID:           docID,
					Title:        title,
					ScorePercent: int(r.Similarity * 100),
					PaperlessURL: strings.TrimRight(paperlessBaseURL, "/") + fmt.Sprintf("/documents/%d/details", docID),
				})
			}
			ragContext = strings.Join(parts, "\n\n")
		}
	}

	// ── 2. Build initial messages ─────────────────────────────────────────────
	systemContent := "You are an assistant for a personal document archive.\n" +
		"Use the provided tools to answer questions about the archive (tags, counts, search, documents).\n" +
		"Be concise and specific. Always respond in the same language the user used."

	if ragContext != "" {
		systemContent += "\n\nRelevant document excerpts from the archive index:\n" + ragContext
	}

	messages := []ollama.ChatMessage{
		{Role: "system", Content: systemContent},
	}
	for _, h := range history {
		messages = append(messages, ollama.ChatMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, ollama.ChatMessage{Role: "user", Content: question})

	// ── 3. Tool-call loop ─────────────────────────────────────────────────────
	ollamaTools := buildTools()
	const maxRounds = 5
	var answer string
	start := time.Now()
	perf := &types.ChatPerf{}

	for round := 0; round < maxRounds; round++ {
		status("Thinking…")
		msg, rp, err := oc.Chat(chatModel, messages, ollamaTools)
		if err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		perf.LLMRounds++
		if rp != nil {
			perf.GeneratedTokens += rp.GeneratedTokens
			perf.TokensPerSec = rp.TokensPerSec // use last round's rate
		}

		messages = append(messages, *msg)

		if len(msg.ToolCalls) == 0 {
			answer = msg.Content
			break
		}

		// Execute each tool call and append results
		for _, tc := range msg.ToolCalls {
			status(fmt.Sprintf("Calling tool: %s…", tc.Function.Name))
			tr, err := tools.Execute(tc.Function.Name, tc.Function.Arguments, pc, paperlessBaseURL)
			text := ""
			if err != nil {
				text = fmt.Sprintf("error: %v", err)
			} else {
				text = tr.Text
				sources = appendUniqueSources(sources, tr.Sources)
			}
			perf.ToolCalls++
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
				Name:        "search_documents",
				Description: "Search for documents in paperless-ngx by keyword or phrase. Returns matching document titles and IDs.",
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
				Description: "Retrieve full details and content of a specific document by its ID.",
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
				Name:        "list_documents_by_type",
				Description: "List all documents with a given document type (e.g. 'Zeugnis', 'Rechnung'). Use list_document_types first if you need to know available types.",
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
				Description: "List all documents from a specific correspondent (sender/recipient). Use list_correspondents first if you need to know available correspondents.",
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
