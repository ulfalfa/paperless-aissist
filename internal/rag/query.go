package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
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
) (*types.ChatResult, error) {

	// ── 1. Vector search for document context ────────────────────────────────
	var ragContext string
	var sources []types.ChatSource

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
	tools := buildTools()
	const maxRounds = 5
	var answer string

	for round := 0; round < maxRounds; round++ {
		msg, err := oc.Chat(chatModel, messages, tools)
		if err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}

		messages = append(messages, *msg)

		if len(msg.ToolCalls) == 0 {
			answer = msg.Content
			break
		}

		// Execute each tool call and append results
		for _, tc := range msg.ToolCalls {
			result := executeTool(tc.Function.Name, tc.Function.Arguments, pc)
			messages = append(messages, ollama.ChatMessage{
				Role:    "tool",
				Content: result,
			})
		}
	}

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
	}
}

// executeTool dispatches a tool call to the paperless client and returns the result as a string.
func executeTool(name string, args json.RawMessage, pc *paperless.Client) string {
	switch name {
	case "list_tags":
		tags, err := pc.ListTagsWithCount()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		var parts []string
		for _, t := range tags {
			parts = append(parts, fmt.Sprintf("%s (ID:%d, %d docs)", t.Name, t.ID, t.Count))
		}
		return fmt.Sprintf("%d tags: %s", len(tags), strings.Join(parts, ", "))

	case "list_correspondents":
		items, err := pc.ListCorrespondents()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		names := make([]string, len(items))
		for i, c := range items {
			names[i] = c.Name
		}
		return fmt.Sprintf("%d correspondents: %s", len(items), strings.Join(names, ", "))

	case "list_document_types":
		items, err := pc.ListDocumentTypes()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		names := make([]string, len(items))
		for i, dt := range items {
			names[i] = dt.Name
		}
		return fmt.Sprintf("%d document types: %s", len(items), strings.Join(names, ", "))

	case "count_documents":
		n, err := pc.CountDocuments()
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return fmt.Sprintf("%d documents", n)

	case "search_documents":
		var p struct {
			Query    string `json:"query"`
			PageSize int    `json:"page_size"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return fmt.Sprintf("error parsing args: %v", err)
		}
		if p.PageSize <= 0 {
			p.PageSize = 10
		}
		docs, err := pc.SearchDocuments(p.Query, p.PageSize)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		if len(docs) == 0 {
			return "no documents found"
		}
		var parts []string
		for _, d := range docs {
			parts = append(parts, fmt.Sprintf("ID:%d %q", d.ID, d.Title))
		}
		return fmt.Sprintf("%d results: %s", len(docs), strings.Join(parts, "; "))

	case "get_document":
		var p struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return fmt.Sprintf("error parsing args: %v", err)
		}
		doc, err := pc.GetDocument(p.ID)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		preview := doc.Content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		return fmt.Sprintf("ID:%d Title:%q Created:%s\n%s", doc.ID, doc.Title, doc.Created, preview)

	default:
		return fmt.Sprintf("unknown tool: %s", name)
	}
}
