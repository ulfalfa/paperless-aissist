package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/types"
)

// Result is the unified return value from Execute.
// Text is a formatted string for use as an LLM tool message.
// Data is the raw Go value for JSON serialization (MCP responses).
// Sources are paperless documents discovered during execution.
type Result struct {
	Text    string
	Data    any
	Sources []types.ChatSource
}

// Execute dispatches a tool call by name and returns the result.
// baseURL is used to construct direct document links for sources.
func Execute(name string, args json.RawMessage, pc *paperless.Client, baseURL string) (*Result, error) {
	docURL := func(id int) string {
		return strings.TrimRight(baseURL, "/") + fmt.Sprintf("/documents/%d/details", id)
	}
	docsToSources := func(docs []types.PaperlessDocument) []types.ChatSource {
		srcs := make([]types.ChatSource, len(docs))
		for i, d := range docs {
			srcs[i] = types.ChatSource{ID: d.ID, Title: d.Title, PaperlessURL: docURL(d.ID)}
		}
		return srcs
	}

	switch name {

	case "list_tags":
		tags, err := pc.ListTags()
		if err != nil {
			return nil, err
		}
		parts := make([]string, len(tags))
		for i, t := range tags {
			parts[i] = fmt.Sprintf("%s (ID:%d, %d docs)", t.Name, t.ID, t.DocumentCount)
		}
		return &Result{
			Text: fmt.Sprintf("%d tags: %s", len(tags), strings.Join(parts, ", ")),
			Data: tags,
		}, nil

	case "list_correspondents":
		items, err := pc.ListCorrespondents()
		if err != nil {
			return nil, err
		}
		names := make([]string, len(items))
		for i, c := range items {
			names[i] = c.Name
		}
		return &Result{
			Text: fmt.Sprintf("%d correspondents: %s", len(items), strings.Join(names, ", ")),
			Data: items,
		}, nil

	case "list_document_types":
		items, err := pc.ListDocumentTypes()
		if err != nil {
			return nil, err
		}
		names := make([]string, len(items))
		for i, dt := range items {
			names[i] = dt.Name
		}
		return &Result{
			Text: fmt.Sprintf("%d document types: %s", len(items), strings.Join(names, ", ")),
			Data: items,
		}, nil

	case "count_documents":
		n, err := pc.CountDocuments()
		if err != nil {
			return nil, err
		}
		return &Result{
			Text: fmt.Sprintf("%d documents", n),
			Data: map[string]int{"count": n},
		}, nil

	case "search_documents":
		var p struct {
			Query    string `json:"query"`
			PageSize int    `json:"page_size"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if p.PageSize <= 0 {
			p.PageSize = 10
		}
		docs, err := pc.SearchDocuments(p.Query, p.PageSize)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: fmt.Sprintf("no documents found for query %q — do not retry with similar queries", p.Query), Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q", d.ID, d.Title)
		}
		return &Result{
			Text:    fmt.Sprintf("%d results: %s", len(docs), strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	case "get_document":
		var p struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		doc, err := pc.GetDocument(p.ID)
		if err != nil {
			return nil, err
		}
		preview := doc.Content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		return &Result{
			Text:    fmt.Sprintf("ID:%d Title:%q Created:%s\n%s", doc.ID, doc.Title, doc.Created, preview),
			Data:    doc,
			Sources: []types.ChatSource{{ID: doc.ID, Title: doc.Title, PaperlessURL: docURL(doc.ID)}},
		}, nil

	case "list_documents_by_type":
		var p struct {
			TypeName string `json:"type_name"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		docTypes, err := pc.ListDocumentTypes()
		if err != nil {
			return nil, err
		}
		nameLower := strings.ToLower(strings.TrimSpace(p.TypeName))
		typeID := -1
		for _, t := range docTypes {
			if strings.ToLower(t.Name) == nameLower {
				typeID = t.ID
				break
			}
		}
		if typeID == -1 {
			return &Result{Text: fmt.Sprintf("document type %q not found", p.TypeName)}, nil
		}
		docs, err := pc.ListDocumentsByType(typeID)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: fmt.Sprintf("no documents with type %q", p.TypeName), Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q (%s)", d.ID, d.Title, d.Created)
		}
		return &Result{
			Text:    fmt.Sprintf("%d documents of type %q: %s", len(docs), p.TypeName, strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	case "list_documents_by_correspondent":
		var p struct {
			CorrespondentName string `json:"correspondent_name"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		correspondents, err := pc.ListCorrespondents()
		if err != nil {
			return nil, err
		}
		nameLower := strings.ToLower(strings.TrimSpace(p.CorrespondentName))
		corrID := -1
		for _, c := range correspondents {
			if strings.ToLower(c.Name) == nameLower {
				corrID = c.ID
				break
			}
		}
		if corrID == -1 {
			return &Result{Text: fmt.Sprintf("correspondent %q not found", p.CorrespondentName)}, nil
		}
		docs, err := pc.ListDocumentsByCorrespondent(corrID)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: fmt.Sprintf("no documents from %q", p.CorrespondentName), Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q (%s)", d.ID, d.Title, d.Created)
		}
		return &Result{
			Text:    fmt.Sprintf("%d documents from %q: %s", len(docs), p.CorrespondentName, strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
