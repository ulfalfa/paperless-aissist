package rag

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/types"
)

// Query embeds the question, finds the top-5 most relevant documents,
// generates an answer using conversation history for context, and returns
// the result with an updated history for the next turn.
func (idx *Indexer) Query(
	question, paperlessBaseURL string,
	oc *ollama.Client,
	embedModel, chatModel string,
	history []types.ConversationMessage,
) (*types.ChatResult, error) {

	emb, err := oc.Embed(embedModel, question)
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}

	ctx := context.Background()
	results, err := idx.store.Search(ctx, emb, 5)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	if len(results) == 0 {
		return &types.ChatResult{
			Answer:  "No relevant documents found in the index. Please index your documents first.",
			History: history,
		}, nil
	}

	// Build document context block
	var contextParts []string
	var sources []types.ChatSource
	for i, r := range results {
		title := r.Metadata["title"]
		docID, _ := strconv.Atoi(r.ID)
		contextParts = append(contextParts, fmt.Sprintf("[%d] %s\n%s", i+1, title, r.Content))
		sources = append(sources, types.ChatSource{
			ID:           docID,
			Title:        title,
			ScorePercent: int(r.Similarity * 100),
			PaperlessURL: strings.TrimRight(paperlessBaseURL, "/") + fmt.Sprintf("/documents/%d/details", docID),
		})
	}

	prompt := buildPrompt(question, strings.Join(contextParts, "\n\n"), history)

	answer, err := oc.Generate(chatModel, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
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

func buildPrompt(question, documentContext string, history []types.ConversationMessage) string {
	var b strings.Builder

	b.WriteString("You are an assistant for a personal document archive.\n")
	b.WriteString("Answer questions using the provided documents and conversation history.\n")
	b.WriteString("Be concise and specific. If you cannot find the answer, say so clearly.\n")
	b.WriteString("Always respond in the same language the user asked their question in.\n\n")

	if len(history) > 0 {
		b.WriteString("Previous conversation:\n")
		for _, msg := range history {
			switch msg.Role {
			case "user":
				b.WriteString("User: " + msg.Content + "\n")
			case "assistant":
				b.WriteString("Assistant: " + msg.Content + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("Documents:\n")
	b.WriteString(documentContext)
	b.WriteString("\n\nQuestion: ")
	b.WriteString(question)
	b.WriteString("\n\nAnswer:")

	return b.String()
}
