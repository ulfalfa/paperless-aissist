package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/types"
)

// AfterUpload is called after a successful upload_document operation.
// Set by main.go to rag.TriggerAsyncRefresh to avoid a circular import.
var AfterUpload func()

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
			parts[i] = fmt.Sprintf("[ID:%d] %s\n%s", d.ID, d.Title, d.Content)
		}
		return &Result{
			Text:    strings.Join(parts, "\n\n"),
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
		return &Result{
			Text:    fmt.Sprintf("ID:%d Title:%q Created:%s\n%s", doc.ID, doc.Title, doc.Created, doc.Content),
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

	case "find_documents":
		var p struct {
			Query            string   `json:"query"`
			CorrespondentName string  `json:"correspondent_name"`
			DocumentTypeName  string  `json:"document_type_name"`
			TagNames          []string `json:"tag_names"`
			CreatedFrom       string  `json:"created_from"`
			CreatedTo         string  `json:"created_to"`
			Ordering          string  `json:"ordering"`
			Limit             int     `json:"limit"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		opts := paperless.FindOptions{
			Query:       p.Query,
			CreatedFrom: p.CreatedFrom,
			CreatedTo:   p.CreatedTo,
			Ordering:    p.Ordering,
			PageSize:    p.Limit,
		}
		if p.CorrespondentName != "" {
			items, err := pc.ListCorrespondents()
			if err != nil {
				return nil, err
			}
			nl := strings.ToLower(strings.TrimSpace(p.CorrespondentName))
			found := false
			for _, item := range items {
				if strings.ToLower(item.Name) == nl {
					id := item.ID
					opts.CorrespondentID = &id
					found = true
					break
				}
			}
			if !found {
				return &Result{Text: fmt.Sprintf("correspondent %q not found", p.CorrespondentName)}, nil
			}
		}
		if p.DocumentTypeName != "" {
			items, err := pc.ListDocumentTypes()
			if err != nil {
				return nil, err
			}
			nl := strings.ToLower(strings.TrimSpace(p.DocumentTypeName))
			found := false
			for _, item := range items {
				if strings.ToLower(item.Name) == nl {
					id := item.ID
					opts.DocumentTypeID = &id
					found = true
					break
				}
			}
			if !found {
				return &Result{Text: fmt.Sprintf("document type %q not found", p.DocumentTypeName)}, nil
			}
		}
		if len(p.TagNames) > 0 {
			tags, err := pc.ListTags()
			if err != nil {
				return nil, err
			}
			tagMap := make(map[string]int, len(tags))
			for _, t := range tags {
				tagMap[strings.ToLower(t.Name)] = t.ID
			}
			for _, name := range p.TagNames {
				id, ok := tagMap[strings.ToLower(strings.TrimSpace(name))]
				if !ok {
					return &Result{Text: fmt.Sprintf("tag %q not found", name)}, nil
				}
				opts.TagIDs = append(opts.TagIDs, id)
			}
		}
		docs, err := pc.FindDocuments(opts)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: "no documents found matching the given criteria", Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q (%s)", d.ID, d.Title, d.Created)
		}
		return &Result{
			Text:    fmt.Sprintf("%d documents: %s", len(docs), strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	case "list_documents_by_tag":
		var p struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		tags, err := pc.ListTags()
		if err != nil {
			return nil, err
		}
		nl := strings.ToLower(strings.TrimSpace(p.TagName))
		tagID := -1
		for _, t := range tags {
			if strings.ToLower(t.Name) == nl {
				tagID = t.ID
				break
			}
		}
		if tagID == -1 {
			return &Result{Text: fmt.Sprintf("tag %q not found", p.TagName)}, nil
		}
		docs, err := pc.ListDocumentsByTag(tagID)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: fmt.Sprintf("no documents with tag %q", p.TagName), Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q (%s)", d.ID, d.Title, d.Created)
		}
		return &Result{
			Text:    fmt.Sprintf("%d documents with tag %q: %s", len(docs), p.TagName, strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	case "list_recent_documents":
		var p struct {
			Limit int    `json:"limit"`
			Since string `json:"since"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if p.Limit <= 0 {
			p.Limit = 10
		}
		docs, err := pc.FindDocuments(paperless.FindOptions{
			Ordering:    "-created",
			PageSize:    p.Limit,
			CreatedFrom: p.Since,
		})
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return &Result{Text: "no recent documents found", Data: docs}, nil
		}
		parts := make([]string, len(docs))
		for i, d := range docs {
			parts[i] = fmt.Sprintf("ID:%d %q (%s)", d.ID, d.Title, d.Created)
		}
		return &Result{
			Text:    fmt.Sprintf("%d recent documents: %s", len(docs), strings.Join(parts, "; ")),
			Data:    docs,
			Sources: docsToSources(docs),
		}, nil

	case "add_tag_to_document":
		var p struct {
			ID              int    `json:"id"`
			TagName         string `json:"tag_name"`
			CreateIfMissing bool   `json:"create_if_missing"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		tags, err := pc.ListTags()
		if err != nil {
			return nil, err
		}
		nl := strings.ToLower(strings.TrimSpace(p.TagName))
		tagID := -1
		for _, t := range tags {
			if strings.ToLower(t.Name) == nl {
				tagID = t.ID
				break
			}
		}
		if tagID == -1 {
			if p.CreateIfMissing {
				id, err := pc.ResolveOrCreate("tag", p.TagName, tags)
				if err != nil {
					return nil, err
				}
				if id == nil {
					return &Result{Text: fmt.Sprintf("could not create tag %q (strict mode?)", p.TagName)}, nil
				}
				tagID = *id
			} else {
				return &Result{Text: fmt.Sprintf("tag %q not found — set create_if_missing=true to create it", p.TagName)}, nil
			}
		}
		if err := pc.AddTags(p.ID, []int{tagID}); err != nil {
			return nil, err
		}
		return &Result{Text: fmt.Sprintf("tag %q added to document %d", p.TagName, p.ID)}, nil

	case "remove_tag_from_document":
		var p struct {
			ID      int    `json:"id"`
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		tags, err := pc.ListTags()
		if err != nil {
			return nil, err
		}
		nl := strings.ToLower(strings.TrimSpace(p.TagName))
		tagID := -1
		for _, t := range tags {
			if strings.ToLower(t.Name) == nl {
				tagID = t.ID
				break
			}
		}
		if tagID == -1 {
			return &Result{Text: fmt.Sprintf("tag %q not found", p.TagName)}, nil
		}
		if err := pc.RemoveTags(p.ID, []int{tagID}); err != nil {
			return nil, err
		}
		return &Result{Text: fmt.Sprintf("tag %q removed from document %d", p.TagName, p.ID)}, nil

	case "download_document":
		var p struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		data, contentType, filename, err := pc.GetDocumentDownload(p.ID)
		if err != nil {
			return nil, err
		}
		result := map[string]string{
			"filename":  filename,
			"mime_type": contentType,
			"content":   base64.StdEncoding.EncodeToString(data),
		}
		j, _ := json.Marshal(result)
		return &Result{Text: string(j)}, nil

	case "upload_document":
		var p struct {
			Filename         string   `json:"filename"`
			Base64Content    string   `json:"base64_content"`
			Title            string   `json:"title"`
			CorrespondentName string  `json:"correspondent_name"`
			DocumentTypeName  string  `json:"document_type_name"`
			TagNames          []string `json:"tag_names"`
			Created           string  `json:"created"`
			Wait              *bool   `json:"wait"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if p.Filename == "" {
			return nil, fmt.Errorf("filename is required")
		}
		if p.Base64Content == "" {
			return nil, fmt.Errorf("base64_content is required")
		}
		data, err := base64.StdEncoding.DecodeString(p.Base64Content)
		if err != nil {
			return nil, fmt.Errorf("base64_content: %w", err)
		}
		opts := paperless.UploadOptions{
			Title:   p.Title,
			Created: p.Created,
		}
		if p.CorrespondentName != "" {
			items, err := pc.ListCorrespondents()
			if err != nil {
				return nil, err
			}
			id, err := pc.ResolveOrCreate("correspondent", p.CorrespondentName, items)
			if err != nil {
				return nil, err
			}
			opts.CorrespondentID = id
		}
		if p.DocumentTypeName != "" {
			items, err := pc.ListDocumentTypes()
			if err != nil {
				return nil, err
			}
			id, err := pc.ResolveOrCreate("document_type", p.DocumentTypeName, items)
			if err != nil {
				return nil, err
			}
			opts.DocumentTypeID = id
		}
		if len(p.TagNames) > 0 {
			tags, err := pc.ListTags()
			if err != nil {
				return nil, err
			}
			for _, name := range p.TagNames {
				id, err := pc.ResolveOrCreate("tag", name, tags)
				if err != nil {
					return nil, err
				}
				if id != nil {
					opts.TagIDs = append(opts.TagIDs, *id)
				}
			}
		}
		taskID, err := pc.UploadDocument(p.Filename, data, opts)
		if err != nil {
			return nil, err
		}
		doWait := p.Wait == nil || *p.Wait
		if !doWait {
			return &Result{
				Text: fmt.Sprintf("document %q uploaded, task_id=%s (not waiting for completion)", p.Filename, taskID),
				Data: map[string]string{"task_id": taskID, "status": "PENDING"},
			}, nil
		}
		status, taskResult, err := pc.WaitForTask(taskID, 60)
		if err != nil {
			return nil, err
		}
		if status == "SUCCESS" && AfterUpload != nil {
			AfterUpload()
		}
		return &Result{
			Text: fmt.Sprintf("document %q uploaded: status=%s result=%s", p.Filename, status, taskResult),
			Data: map[string]string{"task_id": taskID, "status": status, "result": taskResult},
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
