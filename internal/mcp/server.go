package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/types"
	"github.com/ulf/paperless-aissist/internal/validate"
)

func NewHTTPHandler() http.Handler {
	s := server.NewMCPServer("paperless-aissist", "0.1.0")

	// list_tags
	s.AddTool(mcpsdk.NewTool("list_tags",
		mcpsdk.WithDescription("List all tags in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		tags, err := pc.ListTags()
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(tags)
	})

	// list_correspondents
	s.AddTool(mcpsdk.NewTool("list_correspondents",
		mcpsdk.WithDescription("List all correspondents in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		items, err := pc.ListCorrespondents()
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(items)
	})

	// list_document_types
	s.AddTool(mcpsdk.NewTool("list_document_types",
		mcpsdk.WithDescription("List all document types in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		items, err := pc.ListDocumentTypes()
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(items)
	})

	// get_document
	s.AddTool(mcpsdk.NewTool("get_document",
		mcpsdk.WithDescription("Get a document by ID from paperless-ngx"),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		doc, err := pc.GetDocument(id)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(doc)
	})

	// search_documents
	s.AddTool(mcpsdk.NewTool("search_documents",
		mcpsdk.WithDescription("Search for documents in paperless-ngx"),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query")),
		mcpsdk.WithNumber("page_size", mcpsdk.Description("Max results (default 10)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		query := args["query"].(string)
		pageSize := 10
		if ps, ok := args["page_size"].(float64); ok {
			pageSize = int(ps)
		}
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		docs, err := pc.SearchDocuments(query, pageSize)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(docs)
	})

	// analyze_document
	s.AddTool(mcpsdk.NewTool("analyze_document",
		mcpsdk.WithDescription("Analyze a document with Ollama and optionally save results"),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithBoolean("save", mcpsdk.Description("Save results to paperless-ngx (default false)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		save := false
		if sv, ok := args["save"].(bool); ok {
			save = sv
		}
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)

		doc, err := pc.GetDocument(id)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		promptTemplate, err := config.LoadPrompt(cfg.PromptFile)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
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
		result, _, err := oc.Analyze(prompt)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		result = validate.Apply(result, cfg.AITag)

		if save {
			if err := applyAndSave(pc, doc, result, tags, correspondents, docTypes); err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
		}
		return jsonResult(result)
	})

	// update_document
	s.AddTool(mcpsdk.NewTool("update_document",
		mcpsdk.WithDescription("Update a document's metadata in paperless-ngx"),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithString("title", mcpsdk.Description("Document title")),
		mcpsdk.WithString("correspondent", mcpsdk.Description("Correspondent name")),
		mcpsdk.WithString("document_type", mcpsdk.Description("Document type name")),
		mcpsdk.WithString("created_date", mcpsdk.Description("Created date (YYYY-MM-DD)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

		doc, err := pc.GetDocument(id)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}

		title := doc.Title
		if t, ok := args["title"].(string); ok && t != "" {
			title = t
		}
		created := doc.Created
		if d, ok := args["created_date"].(string); ok && d != "" {
			created = d + "T00:00:00Z"
		}

		correspondents, _ := pc.ListCorrespondents()
		docTypes, _ := pc.ListDocumentTypes()

		var corrID *int
		if c, ok := args["correspondent"].(string); ok && c != "" {
			corrID, _ = pc.ResolveOrCreate("correspondent", c, correspondents)
		}
		var dtID *int
		if d, ok := args["document_type"].(string); ok && d != "" {
			dtID, _ = pc.ResolveOrCreate("document_type", d, docTypes)
		}

		update := types.DocumentUpdate{
			Title:         title,
			Created:       created,
			Correspondent: corrID,
			DocumentType:  dtID,
			Tags:          doc.Tags,
		}
		if err := pc.UpdateDocument(id, update); err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return mcpsdk.NewToolResultText(fmt.Sprintf("Document %d updated successfully", id)), nil
	})

	return server.NewStreamableHTTPServer(s)
}

func namesOf(items []types.PaperlessItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}

func jsonResult(v any) (*mcpsdk.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

func applyAndSave(pc *paperless.Client, doc *types.PaperlessDocument, result *types.AnalysisResult, tags, correspondents, docTypes []types.PaperlessItem) error {
	var tagIDs []int
	for _, name := range result.Tags {
		id, err := pc.ResolveOrCreate("tag", name, tags)
		if err == nil && id != nil {
			tagIDs = append(tagIDs, *id)
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
	return pc.UpdateDocument(doc.ID, update)
}
