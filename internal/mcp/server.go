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
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
	"github.com/ulf/paperless-aissist/internal/validate"
)

func NewHTTPHandler() http.Handler {
	s := server.NewMCPServer("paperless-aissist", "0.1.0")

	// helper: run a tool via the shared tools package and return JSON
	runTool := func(name string, args json.RawMessage) (*mcpsdk.CallToolResult, error) {
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		res, err := tools.Execute(name, args, pc, cfg.PaperlessURL)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(res.Data)
	}

	// list_tags
	s.AddTool(mcpsdk.NewTool("list_tags",
		mcpsdk.WithDescription("List all tags in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_tags", nil)
	})

	// list_correspondents
	s.AddTool(mcpsdk.NewTool("list_correspondents",
		mcpsdk.WithDescription("List all correspondents in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_correspondents", nil)
	})

	// list_document_types
	s.AddTool(mcpsdk.NewTool("list_document_types",
		mcpsdk.WithDescription("List all document types in paperless-ngx"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_document_types", nil)
	})

	// count_documents
	s.AddTool(mcpsdk.NewTool("count_documents",
		mcpsdk.WithDescription("Return the total number of documents in the archive"),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("count_documents", nil)
	})

	// get_document
	s.AddTool(mcpsdk.NewTool("get_document",
		mcpsdk.WithDescription("Get a document by ID from paperless-ngx"),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"id": args["id"]})
		return runTool("get_document", b)
	})

	// search_documents
	s.AddTool(mcpsdk.NewTool("search_documents",
		mcpsdk.WithDescription("Search for documents in paperless-ngx"),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query")),
		mcpsdk.WithNumber("page_size", mcpsdk.Description("Max results (default 10)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"query": args["query"], "page_size": args["page_size"]})
		return runTool("search_documents", b)
	})

	// list_documents_by_type
	s.AddTool(mcpsdk.NewTool("list_documents_by_type",
		mcpsdk.WithDescription("List all documents with a given document type (e.g. 'Zeugnis', 'Rechnung')"),
		mcpsdk.WithString("type_name", mcpsdk.Required(), mcpsdk.Description("Document type name (case-insensitive)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"type_name": args["type_name"]})
		return runTool("list_documents_by_type", b)
	})

	// list_documents_by_correspondent
	s.AddTool(mcpsdk.NewTool("list_documents_by_correspondent",
		mcpsdk.WithDescription("List all documents from a specific correspondent"),
		mcpsdk.WithString("correspondent_name", mcpsdk.Required(), mcpsdk.Description("Correspondent name (case-insensitive)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"correspondent_name": args["correspondent_name"]})
		return runTool("list_documents_by_correspondent", b)
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
		result, _, err := oc.Analyze(prompt, types.ModelOptions{})
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

	// rag_search
	s.AddTool(mcpsdk.NewTool("rag_search",
		mcpsdk.WithDescription("Search documents using semantic vector search (RAG / embeddings)"),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Natural language query")),
		mcpsdk.WithNumber("max_results", mcpsdk.Description("Max results (default 5)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		query, _ := args["query"].(string)
		k := 5
		if n, ok := args["max_results"].(float64); ok && n > 0 {
			k = int(n)
		}
		idx := rag.Global()
		if idx == nil {
			return mcpsdk.NewToolResultError("RAG index not initialized"), nil
		}
		cfg, _ := config.Load()
		results, err := idx.SearchText(ctx, cfg.OllamaHost, cfg.EmbedModel, query, k)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(results)
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
